/*
** Copyright (C) 2026-2026 Pylon One Ltd; James Botting
**
** Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
** documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
** rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to
** permit persons to whom the Software is furnished to do so, subject to the following conditions:
**
** The above copyright notice and this permission notice shall be included in all copies or substantial portions
** of the Software.
**
** THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
** WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
** COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
** TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
** SOFTWARE.
**/

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"reflect"
	"time"

	"golang.zabbix.com/sdk/errs"
	"golang.zabbix.com/sdk/log"
	"golang.zabbix.com/sdk/plugin"
	"golang.zabbix.com/sdk/plugin/container"
)

const (
	// Name of the plugin.
	Name = "KeaDHCP"

	serviceDHCP4 = "dhcp4"
	serviceDHCP6 = "dhcp6"

	networkHTTP = "http"
	networkUnix = "unix"

	defaultTimeout = 5 * time.Second
)

var (
	_ plugin.Configurator = (*KeaPlugin)(nil)
	_ plugin.Exporter     = (*KeaPlugin)(nil)
	_ plugin.Runner       = (*KeaPlugin)(nil)
)

// Static list of permitted commands. Maybe move this to a config option later?
//
//nolint:gochecknoglobals // read-only lookup table
var allowedCommands = map[string]bool{
	"statistic-get-all": true,
	"statistic-get":     true,
	"status-get":        true,
	"version-get":       true,
	"build-get":         true,
	"ha-status-get":     true,
}

// KeaPlugin is a structure that implements necessary interfaces for plugin work.
type KeaPlugin struct {
	plugin.Base

	config *pluginConfig
}

// KeaRequest maps the JSON command payload required by the Kea control API.
type KeaRequest struct {
	Command   string            `json:"command"`
	Service   []string          `json:"service,omitempty"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// KeaResponse is a single Kea control API answer.
type KeaResponse struct {
	Result    int             `json:"result"`
	Text      string          `json:"text"`
	Arguments json.RawMessage `json:"arguments"`
}

// Subnet is the breakdown of subnet and name, how Zabbix determines pool names for LLD.
type Subnet struct {
	ID            int64  `json:"id"`
	Subnet        string `json:"subnet"`
	Name          string `json:"name"`
	SharedNetwork string `json:"shared_network,omitempty"`
}

// The types below follow the layout of Kea's config-get answer, so their JSON
// names are Kea's rather than ours.
//
//nolint:tagliatelle // Kea configuration keys
type keaConfig struct {
	Dhcp4 *keaServer `json:"Dhcp4"`
	Dhcp6 *keaServer `json:"Dhcp6"`
}

//nolint:tagliatelle // Kea configuration keys
type keaServer struct {
	Subnet4        []keaSubnet        `json:"subnet4"`
	Subnet6        []keaSubnet        `json:"subnet6"`
	SharedNetworks []keaSharedNetwork `json:"shared-networks"`
}

type keaSharedNetwork struct {
	Name    string      `json:"name"`
	Subnet4 []keaSubnet `json:"subnet4"`
	Subnet6 []keaSubnet `json:"subnet6"`
}

//nolint:tagliatelle // Kea configuration keys
type keaSubnet struct {
	ID          int64          `json:"id"`
	Subnet      string         `json:"subnet"`
	UserContext keaUserContext `json:"user-context"`
}

type keaUserContext struct {
	Name string `json:"name"`
}

// Launch launches the plugin. Blocks until plugin execution has finished.
func Launch() error {
	p := &KeaPlugin{}

	err := p.registerMetrics()
	if err != nil {
		return err
	}

	h, err := container.NewHandler(Name)
	if err != nil {
		return errs.Wrap(err, "failed to create new handler")
	}

	p.Logger = handlerLogger(&h)

	err = h.Execute()
	if err != nil {
		return errs.Wrap(err, "failed to execute plugin handler")
	}

	return nil
}

// Start starts the Kea plugin. Is required for plugin to match runner interface.
func (p *KeaPlugin) Start() {
	p.Infof("Start called")
}

// Stop stops the Kea plugin. Is required for plugin to match runner interface.
func (p *KeaPlugin) Stop() {
	p.Infof("Stop called")
}

// Export runs when Zabbix server polls the item key.
func (p *KeaPlugin) Export(key string, params []string, _ plugin.ContextProvider) (any, error) {
	if p.config == nil {
		return nil, errs.New("unable to load plugin configuration")
	}

	if len(params) == 0 || params[0] == "" {
		return nil, errs.New("missing session name parameter")
	}

	sess, ok := p.config.Sessions[params[0]]
	if !ok {
		return nil, errs.Errorf(
			"unknown session %q: define Plugins.%s.Sessions.%s.Uri in the agent configuration",
			params[0], Name, params[0],
		)
	}

	switch key {
	case "kea.stats":
		return p.exportStats(sess, params[1:])
	case "kea.subnets":
		return p.exportSubnets(sess, params[1:])
	default:
		return nil, plugin.UnsupportedMetricError
	}
}

// Register metrics. Note for self, if the description doesn't end in a .
// Then the plugin doesn't load properly!
func (p *KeaPlugin) registerMetrics() error {
	err := plugin.RegisterMetrics(
		p, Name,
		"kea.stats", "Returns the raw JSON answer to a read-only Kea command.",
		"kea.subnets", "Returns the configured subnets (ID, prefix, name) of a Kea DHCP daemon as JSON.",
	)
	if err != nil {
		return errs.Wrap(err, "failed to register metrics")
	}

	return nil
}

// exportStats handles kea.stats[<session>,<command>,<service>].
func (p *KeaPlugin) exportStats(sess session, params []string) (any, error) {
	cmd := "statistic-get-all"
	if len(params) > 0 && params[0] != "" {
		cmd = params[0]
	}

	if !allowedCommands[cmd] {
		return nil, errs.Errorf("command '%s' is not in the read-only whitelist", cmd)
	}

	service := serviceDHCP4
	if len(params) > 1 && params[1] != "" {
		service = params[1]
	}

	respBytes, err := p.query(sess, KeaRequest{Command: cmd, Service: []string{service}})
	if err != nil {
		return nil, err
	}

	return string(respBytes), nil
}

// exportSubnets handles kea.subnets[<session>,<service>].
func (p *KeaPlugin) exportSubnets(sess session, params []string) (any, error) {
	service := serviceDHCP4
	if len(params) > 0 && params[0] != "" {
		service = params[0]
	}

	if service != serviceDHCP4 && service != serviceDHCP6 {
		return nil, errs.Errorf("unsupported service %q: kea.subnets supports dhcp4 and dhcp6", service)
	}

	respBytes, err := p.query(sess, KeaRequest{Command: "config-get", Service: []string{service}})
	if err != nil {
		return nil, err
	}

	subnets, err := parseSubnets(respBytes, service)
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(subnets)
	if err != nil {
		return nil, errs.Wrap(err, "failed to marshal subnet list")
	}

	return string(out), nil
}

// query sends one command to a configured session and returns the raw answer.
func (p *KeaPlugin) query(sess session, reqData KeaRequest) ([]byte, error) {
	network, addr, err := parseURI(sess.URI)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(reqData)
	if err != nil {
		return nil, errs.Wrap(err, "failed to marshal JSON request")
	}

	timeout := time.Duration(p.config.Timeout) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Kea allows control sockets via HTTP/REST or native Unix Domain Sockets
	if network == networkHTTP {
		return queryHTTP(ctx, addr, payload)
	}

	return queryUnix(ctx, addr, payload)
}

// handlerLogger returns the container handler as a logger. Older SDK branches'
// NewHandler returns a handler value and newer ones return a *Handler, so the
// logger is either the pointer we were given or the value it points to.
//
//nolint:ireturn // the SDK handler is only usable as a log.Logger here
func handlerLogger(h any) log.Logger {
	if l, ok := h.(log.Logger); ok {
		return l
	}

	if l, ok := reflect.TypeAssert[log.Logger](reflect.ValueOf(h).Elem()); ok {
		return l
	}

	panic("plugin handler does not implement log.Logger")
}

// parseSubnets extracts the subnet list from a config-get answer, including
// subnets nested in shared networks. Kea errors are returned as errors so a
// failed call never looks like "no subnets" to LLD.
func parseSubnets(raw []byte, service string) ([]Subnet, error) {
	server, err := decodeServer(raw, service)
	if err != nil {
		return nil, err
	}

	subnets := appendSubnets([]Subnet{}, server.subnets(service), "")

	for _, sn := range server.SharedNetworks {
		subnets = appendSubnets(subnets, sn.subnets(service), sn.Name)
	}

	return subnets, nil
}

// decodeServer returns the server section of a config-get answer for the
// given service.
func decodeServer(raw []byte, service string) (*keaServer, error) {
	resp, err := decodeResponse(raw)
	if err != nil {
		return nil, err
	}

	if resp.Result != 0 {
		return nil, errs.Errorf("kea returned result %d: %s", resp.Result, resp.Text)
	}

	var cfg keaConfig

	err = json.Unmarshal(resp.Arguments, &cfg)
	if err != nil {
		return nil, errs.Wrap(err, "failed to parse Kea configuration")
	}

	server, root := cfg.Dhcp4, "Dhcp4"
	if service == serviceDHCP6 {
		server, root = cfg.Dhcp6, "Dhcp6"
	}

	if server == nil {
		return nil, errs.Errorf("kea configuration has no %s section", root)
	}

	return server, nil
}

func appendSubnets(subnets []Subnet, list []keaSubnet, sharedNetwork string) []Subnet {
	for _, s := range list {
		name := s.Subnet
		if s.UserContext.Name != "" {
			name = s.UserContext.Name
		}

		subnets = append(
			subnets,
			Subnet{ID: s.ID, Subnet: s.Subnet, Name: name, SharedNetwork: sharedNetwork},
		)
	}

	return subnets
}

// decodeResponse accepts both a direct daemon answer (an object) and a
// Control Agent answer (an array with one object per service).
func decodeResponse(raw []byte) (KeaResponse, error) {
	var resp KeaResponse

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var list []KeaResponse

		err := json.Unmarshal(trimmed, &list)
		if err != nil {
			return resp, errs.Wrap(err, "failed to parse Kea response")
		}

		if len(list) == 0 {
			return resp, errs.New("empty Kea response")
		}

		return list[0], nil
	}

	err := json.Unmarshal(trimmed, &resp)
	if err != nil {
		return resp, errs.Wrap(err, "failed to parse Kea response")
	}

	return resp, nil
}

func queryHTTP(ctx context.Context, url string, payload []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		return nil, errs.Wrap(err, "failed to create HTTP request")
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errs.Wrap(err, "HTTP request failed")
	}

	defer resp.Body.Close() //nolint:errcheck // nothing to do if close fails after a read

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errs.Wrap(err, "failed to read HTTP response")
	}

	return respBytes, nil
}

// queryUnix talks to a Kea control socket. Unix domain sockets are the most
// common setup for internal deployments.
func queryUnix(ctx context.Context, addr string, payload []byte) ([]byte, error) {
	var d net.Dialer

	conn, err := d.DialContext(ctx, networkUnix, addr)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to connect to unix socket %s", addr)
	}

	defer conn.Close() //nolint:errcheck // nothing to do if close fails after a read

	deadline, _ := ctx.Deadline()

	err = conn.SetDeadline(deadline)
	if err != nil {
		return nil, errs.Wrap(err, "failed to set socket deadline")
	}

	_, err = conn.Write(payload)
	if err != nil {
		return nil, errs.Wrap(err, "failed to write to unix socket")
	}

	respBytes, err := io.ReadAll(conn)
	if err != nil {
		return nil, errs.Wrap(err, "failed to read from unix socket")
	}

	return respBytes, nil
}

func (s *keaServer) subnets(service string) []keaSubnet {
	if service == serviceDHCP6 {
		return s.Subnet6
	}

	return s.Subnet4
}

func (sn *keaSharedNetwork) subnets(service string) []keaSubnet {
	if service == serviceDHCP6 {
		return sn.Subnet6
	}

	return sn.Subnet4
}
