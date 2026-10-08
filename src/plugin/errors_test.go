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
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.zabbix.com/sdk/plugin"
)

// silentKea accepts connections on a Unix socket and never answers.
func silentKea(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "s")

	l, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			// Hold the connection open until the client gives up.
			go func() {
				_, _ = io.Copy(io.Discard, c)
				_ = c.Close()
			}()
		}
	}()

	return path
}

func TestExportNotConfigured(t *testing.T) {
	t.Parallel()

	p := &KeaPlugin{}

	_, err := p.Export("kea.stats", []string{serviceDHCP4}, nil)
	if err == nil || !strings.Contains(err.Error(), "Unable to load plugin configuration") {
		t.Fatalf("err = %v", err)
	}
}

// A configuration that fails to unmarshal must not leave the plugin in a
// state where Export panics.
func TestExportAfterFailedConfigure(t *testing.T) {
	t.Parallel()

	p := &KeaPlugin{Base: plugin.Base{Logger: &countingLogger{}}}
	p.Configure(&plugin.GlobalOptions{Timeout: 3}, []byte("Timeout=2\ninvalid"))

	_, err := p.Export("kea.stats", []string{serviceDHCP4}, nil)
	if err == nil || !strings.Contains(err.Error(), "Unknown session") {
		t.Fatalf("err = %v", err)
	}
}

func TestExportUnknownSession(t *testing.T) {
	t.Parallel()

	p := newPlugin(map[string]session{serviceDHCP4: {URI: missingSocket}})

	_, err := p.Export("kea.stats", []string{serviceDHCP6}, nil)
	if err == nil || !strings.Contains(err.Error(), "Plugins.KeaDHCP.Sessions.dhcp6.Uri") {
		t.Fatalf("err = %v", err)
	}
}

func TestExportUnsupportedKey(t *testing.T) {
	t.Parallel()

	p := newPlugin(map[string]session{serviceDHCP4: {URI: missingSocket}})

	_, err := p.Export("kea.other", []string{serviceDHCP4}, nil)
	if !errors.Is(err, plugin.UnsupportedMetricError) {
		t.Fatalf("err = %v", err)
	}
}

func TestExportStatsCommandAndService(t *testing.T) {
	t.Parallel()

	answer := `[{"result":0,"arguments":{"pid":42}}]`
	path, req := fakeKea(t, answer)
	p := newPlugin(map[string]session{serviceDHCP6: {URI: path}})

	got, err := p.Export("kea.stats", []string{serviceDHCP6, "status-get", serviceDHCP6}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got != answer {
		t.Fatalf("got %v", got)
	}

	if *req != `{"command":"status-get","service":["dhcp6"]}` {
		t.Fatalf("request = %s", *req)
	}
}

func TestExportStatsConnectError(t *testing.T) {
	t.Parallel()

	p := newPlugin(map[string]session{serviceDHCP4: {URI: filepath.Join(t.TempDir(), "missing")}})

	_, err := p.Export("kea.stats", []string{serviceDHCP4}, nil)
	if err == nil || !strings.Contains(err.Error(), "Failed to connect to unix socket") {
		t.Fatalf("err = %v", err)
	}
}

func TestExportSubnetsConnectError(t *testing.T) {
	t.Parallel()

	p := newPlugin(map[string]session{serviceDHCP4: {URI: filepath.Join(t.TempDir(), "missing")}})

	_, err := p.Export("kea.subnets", []string{serviceDHCP4}, nil)
	if err == nil {
		t.Fatal("kea.subnets succeeded without a socket")
	}
}

// Sessions are checked by Validate, but query must still refuse a bad URI if
// one reaches it.
func TestQueryRejectsInvalidSessionURI(t *testing.T) {
	t.Parallel()

	p := newPlugin(map[string]session{"bad": {URI: "ftp://x/"}})

	_, err := p.Export("kea.stats", []string{"bad"}, nil)
	if err == nil || !strings.Contains(err.Error(), "Unsupported URI scheme") {
		t.Fatalf("err = %v", err)
	}
}

func TestQueryTimesOut(t *testing.T) {
	t.Parallel()

	p := &KeaPlugin{config: &pluginConfig{Timeout: 1, Sessions: map[string]session{serviceDHCP4: {URI: silentKea(t)}}}}

	start := time.Now()

	_, err := p.Export("kea.stats", []string{serviceDHCP4}, nil)
	if err == nil || !strings.Contains(err.Error(), "Failed to read from unix socket") {
		t.Fatalf("err = %v", err)
	}

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("query took %s with a 1s timeout", elapsed)
	}
}

// With no timeout configured, query falls back to its own default.
func TestQueryDefaultTimeout(t *testing.T) {
	t.Parallel()

	answer := `{"result":0}`
	path, _ := fakeKea(t, answer)
	p := &KeaPlugin{config: &pluginConfig{Sessions: map[string]session{serviceDHCP4: {URI: path}}}}

	got, err := p.Export("kea.stats", []string{serviceDHCP4}, nil)
	if err != nil || got != answer {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestParseURIInvalidURL(t *testing.T) {
	t.Parallel()

	_, _, err := parseURI("unix://[::1")
	if err == nil {
		t.Fatal("parseURI accepted an unparsable URI")
	}
}

func TestDecodeResponse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, in string
		result   int
		text     string
		ok       bool
	}{
		{"object", `{"result":0,"text":"ok"}`, 0, "ok", true},
		{"array", `[{"result":1,"text":"first"},{"result":0}]`, 1, "first", true},
		{"leading whitespace", " \n[{\"result\":3}]", 3, "", true},
		{"empty array", `[]`, 0, "", false},
		{"bad array", `[{"result":"x"}]`, 0, "", false},
		{"bad object", `{"result":`, 0, "", false},
		{"empty", ``, 0, "", false},
	}

	for _, c := range cases {
		resp, err := decodeResponse([]byte(c.in))
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)

			continue
		}

		if c.ok && (resp.Result != c.result || resp.Text != c.text) {
			t.Errorf("%s: got %+v", c.name, resp)
		}
	}
}

func TestParseSubnetsDhcp6SharedNetworks(t *testing.T) {
	t.Parallel()

	raw := `{"result":0,"arguments":{"Dhcp6":{
	  "subnet6":[{"id":1,"subnet":"2001:db8:1::/64","user-context":{"name":"Lab"}}],
	  "shared-networks":[{"name":"campus","subnet6":[{"id":2,"subnet":"2001:db8:2::/64"}]}]
	}}}`

	got, err := parseSubnets([]byte(raw), serviceDHCP6)
	if err != nil {
		t.Fatal(err)
	}

	want := []Subnet{
		{ID: 1, Subnet: "2001:db8:1::/64", Name: "Lab"},
		{ID: 2, Subnet: "2001:db8:2::/64", Name: "2001:db8:2::/64", SharedNetwork: "campus"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseSubnetsEmpty(t *testing.T) {
	t.Parallel()

	got, err := parseSubnets([]byte(`{"result":0,"arguments":{"Dhcp4":{}}}`), serviceDHCP4)
	if err != nil {
		t.Fatal(err)
	}
	// An empty list, not null, so LLD sees "no subnets" rather than an error.
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestParseSubnetsErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ raw, service, want string }{
		"kea error":       {`{"result":1,"text":"no such command"}`, serviceDHCP4, "no such command"},
		"missing section": {`{"result":0,"arguments":{"Dhcp4":{}}}`, serviceDHCP6, "no Dhcp6 section"},
		"bad arguments":   {`{"result":0,"arguments":[1,2]}`, serviceDHCP4, "Failed to parse Kea configuration"},
		"bad response":    {`not json`, serviceDHCP4, "Failed to parse Kea response"},
	}

	for name, c := range cases {
		_, err := parseSubnets([]byte(c.raw), c.service)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, c.want)
		}
	}
}
