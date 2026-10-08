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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.zabbix.com/sdk/plugin"
	"golang.zabbix.com/sdk/plugin/container"
)

// countingLogger records how many messages were logged at Info level.
type countingLogger struct{ infos []string }

func (*countingLogger) Tracef(string, ...any)   {}
func (*countingLogger) Debugf(string, ...any)   {}
func (*countingLogger) Warningf(string, ...any) {}
func (*countingLogger) Errf(string, ...any)     {}
func (*countingLogger) Critf(string, ...any)    {}

func (l *countingLogger) Infof(format string, _ ...any) {
	l.infos = append(l.infos, format)
}

// setArgs replaces os.Args for the duration of a test.
func setArgs(t *testing.T, args ...string) {
	t.Helper()

	saved := os.Args
	os.Args = args

	t.Cleanup(func() { os.Args = saved })
}

//nolint:paralleltest // changes os.Args or the global metric registry
func TestRegisterMetrics(t *testing.T) {
	plugin.ClearRegistry()
	t.Cleanup(plugin.ClearRegistry)

	p := &KeaPlugin{}

	err := p.registerMetrics()
	if err != nil {
		t.Fatalf("registerMetrics: %v", err)
	}

	for _, key := range []string{"kea.stats", "kea.subnets"} {
		var acc plugin.Accessor

		acc, err = plugin.Get(key)
		if err != nil {
			t.Fatalf("metric %s not registered: %v", key, err)
		}

		if acc != p {
			t.Fatalf("metric %s registered to %v, want the plugin", key, acc)
		}
	}

	acc, err := plugin.GetByName(Name)
	if err != nil || acc != p {
		t.Fatalf("GetByName(%q) = %v, %v", Name, acc, err)
	}

	// A second registration in the same process must fail rather than
	// silently replace the first.
	err = (&KeaPlugin{}).registerMetrics()
	if err == nil {
		t.Fatal("registerMetrics accepted a duplicate registration")
	}
}

// The handler the SDK returns must convert to a logger the way Launch does it.
// On release/6.4 NewHandler returns a value and on release/6.0 a pointer, so
// this guards against passing the wrong shape to handlerLogger.
//
//nolint:paralleltest // changes os.Args or the global metric registry
func TestHandlerLoggerFromSDKHandler(t *testing.T) {
	setArgs(t, "kea-plugin", "/nonexistent/agent.sock")

	h, err := container.NewHandler(Name)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	if handlerLogger(&h) == nil {
		t.Fatal("no logger from the SDK handler")
	}
}

func TestHandlerLoggerRejectsNonLogger(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("handlerLogger accepted a value that is not a logger")
		}
	}()

	s := "not a logger"
	handlerLogger(&s)
}

//nolint:paralleltest // changes os.Args or the global metric registry
func TestLaunchWithoutSocket(t *testing.T) {
	plugin.ClearRegistry()
	t.Cleanup(plugin.ClearRegistry)
	setArgs(t, "kea-plugin")

	err := Launch()
	if err == nil || !strings.Contains(err.Error(), "Failed to create new handler") {
		t.Fatalf("Launch() error = %v", err)
	}
}

//nolint:paralleltest // changes os.Args or the global metric registry
func TestLaunchDuplicateRegistration(t *testing.T) {
	plugin.ClearRegistry()
	t.Cleanup(plugin.ClearRegistry)
	setArgs(t, "kea-plugin")

	err := (&KeaPlugin{}).registerMetrics()
	if err != nil {
		t.Fatal(err)
	}

	err = Launch()
	if err == nil || !strings.Contains(err.Error(), "Failed to register metrics") {
		t.Fatalf("Launch() error = %v", err)
	}
}

// Launch must get as far as connecting to the agent. The SDK retries the
// connection for its own timeout (3s), so this is skipped with -short.
//
//nolint:paralleltest // changes os.Args or the global metric registry
func TestLaunchUnreachableAgentSocket(t *testing.T) {
	// On darwin the SDK's setConnection is a stub that always succeeds, so
	// Execute would go on to read from a nil connection and panic.
	if runtime.GOOS != "linux" {
		t.Skipf("the SDK only connects to the agent socket on linux, not %s", runtime.GOOS)
	}

	if testing.Short() {
		t.Skip("waits for the SDK's connection timeout")
	}

	plugin.ClearRegistry()
	t.Cleanup(plugin.ClearRegistry)
	setArgs(t, "kea-plugin", filepath.Join(t.TempDir(), "agent.sock"))

	err := Launch()
	if err == nil || !strings.Contains(err.Error(), "Failed to execute plugin handler") {
		t.Fatalf("Launch() error = %v", err)
	}
}

func TestStartStop(t *testing.T) {
	t.Parallel()

	l := &countingLogger{}
	p := &KeaPlugin{Base: plugin.Base{Logger: l}}

	p.Start()
	p.Stop()

	if len(l.infos) != 2 {
		t.Fatalf("logged %d info messages, want 2: %v", len(l.infos), l.infos)
	}
}
