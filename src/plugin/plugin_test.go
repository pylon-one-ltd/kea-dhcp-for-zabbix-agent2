package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.zabbix.com/sdk/plugin"
)

const (
	missingSocket = "/nonexistent"
	kea4Socket    = "/run/kea/kea4-ctrl-socket"
	kea4URI       = "unix://" + kea4Socket
)

const sampleConfig = `{"result":0,"arguments":{"Dhcp4":{
  "lease-database":{"type":"mysql","user":"kea","password":"s3cret-db"},
  "subnet4":[
    {"id":1,"subnet":"10.0.0.0/24"},
    {"id":2,"subnet":"10.0.1.0/24","user-context":{"name":"Office"}}
  ],
  "shared-networks":[
    {"name":"campus","subnet4":[{"id":10,"subnet":"10.1.0.0/24"}]}
  ]
}}}`

type ptrLogger struct{}

// fakeKea serves one canned answer per connection on a Unix socket and
// records the last request it received.
func fakeKea(t *testing.T, answer string) (string, *string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "s")

	l, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Close() })

	var req string

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			var buf [4096]byte

			n, _ := c.Read(buf[:])
			req = string(buf[:n])
			_, _ = io.WriteString(c, answer)
			_ = c.Close()
		}
	}()

	return path, &req
}

func (*ptrLogger) Tracef(string, ...any)   {}
func (*ptrLogger) Debugf(string, ...any)   {}
func (*ptrLogger) Warningf(string, ...any) {}
func (*ptrLogger) Infof(string, ...any)    {}
func (*ptrLogger) Errf(string, ...any)     {}
func (*ptrLogger) Critf(string, ...any)    {}

func newPlugin(sessions map[string]session) *KeaPlugin {
	return &KeaPlugin{config: &pluginConfig{Timeout: 2, Sessions: sessions}}
}

func TestOptionsUnmarshal(t *testing.T) {
	t.Parallel()

	cfg := []byte("System.Path=/usr/libexec/zabbix/zabbix-kea-plugin\n" +
		"Timeout=5\n" +
		"Sessions.dhcp4.Uri=unix:///run/kea/kea4-ctrl-socket\n" +
		"Sessions.dhcp6.Uri=/run/kea/kea6-ctrl-socket\n")

	p := &KeaPlugin{}

	err := p.Validate(cfg)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	p.Configure(&plugin.GlobalOptions{Timeout: 3}, cfg)

	if p.config.Timeout != 5 || p.config.System.Path != "/usr/libexec/zabbix/zabbix-kea-plugin" {
		t.Fatalf("unexpected options: %+v", p.config)
	}

	if got := p.config.Sessions[serviceDHCP4].URI; got != kea4URI {
		t.Fatalf("dhcp4 URI = %q", got)
	}

	err = p.Validate([]byte("Sessions.bad.Uri=ftp://x/\n"))
	if err == nil {
		t.Fatal("Validate accepted an ftp URI")
	}
}

func TestParseURI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, network, addr string
		ok                bool
	}{
		{kea4URI, "unix", kea4Socket, true},
		{kea4Socket, "unix", kea4Socket, true},
		{"http://127.0.0.1:8000/", "", "", false},
		{"https://kea.example/", "", "", false},
		{"unix://host/path", "", "", false},
		{"relative/path", "", "", false},
		{"ftp://x/", "", "", false},
		{"", "", "", false},
	}

	for _, c := range cases {
		network, addr, err := parseURI(c.in)
		if (err == nil) != c.ok || network != c.network || addr != c.addr {
			t.Errorf("parseURI(%q) = %q, %q, %v", c.in, network, addr, err)
		}
	}
}

func TestExportRejectsRawTargets(t *testing.T) {
	t.Parallel()

	p := newPlugin(map[string]session{serviceDHCP4: {URI: missingSocket}})

	for _, target := range []string{"/tmp/kea4-ctrl-socket", "http://169.254.169.254/latest/meta-data/", ""} {
		_, err := p.Export("kea.stats", []string{target}, nil)
		if err == nil {
			t.Errorf("Export accepted raw target %q", target)
		}
	}
}

func TestExportRejectsConfigGet(t *testing.T) {
	t.Parallel()

	path, _ := fakeKea(t, sampleConfig)
	p := newPlugin(map[string]session{serviceDHCP4: {URI: "unix://" + path}})

	_, err := p.Export("kea.stats", []string{serviceDHCP4, "config-get", serviceDHCP4}, nil)
	if err == nil || !strings.Contains(err.Error(), "whitelist") {
		t.Fatalf("config-get via kea.stats: err = %v", err)
	}
}

func TestExportStats(t *testing.T) {
	t.Parallel()

	answer := `{"result":0,"arguments":{"pkt4-received":[[5,"2026-10-07 07:00:00.000000"]]}}`
	path, req := fakeKea(t, answer)
	p := newPlugin(map[string]session{serviceDHCP4: {URI: path}})

	got, err := p.Export("kea.stats", []string{serviceDHCP4, "", ""}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got != answer {
		t.Fatalf("got %v", got)
	}

	if *req != `{"command":"statistic-get-all","service":["dhcp4"]}` {
		t.Fatalf("request = %s", *req)
	}
}

func TestExportSubnets(t *testing.T) {
	t.Parallel()

	path, req := fakeKea(t, sampleConfig)
	p := newPlugin(map[string]session{serviceDHCP4: {URI: "unix://" + path}})

	got, err := p.Export("kea.subnets", []string{serviceDHCP4, serviceDHCP4}, nil)
	if err != nil {
		t.Fatal(err)
	}

	out, ok := got.(string)
	if !ok {
		t.Fatalf("got %T, want string", got)
	}

	if strings.Contains(out, "s3cret") || strings.Contains(out, "lease-database") {
		t.Fatalf("kea.subnets leaked configuration: %s", out)
	}

	if !strings.Contains(*req, `"config-get"`) {
		t.Fatalf("request = %s", *req)
	}

	var subnets []Subnet

	err = json.Unmarshal([]byte(out), &subnets)
	if err != nil {
		t.Fatal(err)
	}

	want := []Subnet{
		{ID: 1, Subnet: "10.0.0.0/24", Name: "10.0.0.0/24"},
		{ID: 2, Subnet: "10.0.1.0/24", Name: "Office"},
		{ID: 10, Subnet: "10.1.0.0/24", Name: "10.1.0.0/24", SharedNetwork: "campus"},
	}
	if len(subnets) != len(want) {
		t.Fatalf("got %+v", subnets)
	}

	for i := range want {
		if subnets[i] != want[i] {
			t.Errorf("subnet %d = %+v, want %+v", i, subnets[i], want[i])
		}
	}
}

func TestExportSubnetsKeaError(t *testing.T) {
	t.Parallel()

	path, _ := fakeKea(t, `[{"result":1,"text":"unable to forward command"}]`)
	p := newPlugin(map[string]session{serviceDHCP6: {URI: path}})

	_, err := p.Export("kea.subnets", []string{serviceDHCP6, serviceDHCP6}, nil)
	if err == nil || !strings.Contains(err.Error(), "unable to forward command") {
		t.Fatalf("err = %v", err)
	}
}

func TestExportSubnetsRejectsOtherServices(t *testing.T) {
	t.Parallel()

	p := newPlugin(map[string]session{"d2": {URI: missingSocket}})

	_, err := p.Export("kea.subnets", []string{"d2", "d2"}, nil)
	if err == nil {
		t.Fatal("kea.subnets accepted service d2")
	}
}

// HTTP sessions are disabled in parseURI for now, so they must be rejected
// before any request reaches the server.
func TestExportSubnetsHTTP(t *testing.T) {
	t.Parallel()

	called := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = io.WriteString(w, `[{"result":0,"arguments":{"Dhcp6":{"subnet6":[{"id":7,"subnet":"2001:db8::/64"}]}}}]`)
	}))
	defer srv.Close()

	p := newPlugin(map[string]session{"ca": {URI: srv.URL}})

	_, err := p.Export("kea.subnets", []string{"ca", serviceDHCP6}, nil)
	if err == nil {
		t.Fatal("kea.subnets accepted an http session")
	}

	if called {
		t.Fatal("request was sent to an http session")
	}
}

func TestHandlerLogger(t *testing.T) {
	t.Parallel()

	// Older SDKs: NewHandler returns a value whose pointer is the logger.
	v := ptrLogger{}
	if handlerLogger(&v) == nil {
		t.Fatal("no logger from *handler")
	}

	// Newer SDKs: NewHandler returns a pointer, so &h is a pointer to the logger.
	p := &ptrLogger{}
	if handlerLogger(&p) != p {
		t.Fatal("no logger from **Handler")
	}
}
