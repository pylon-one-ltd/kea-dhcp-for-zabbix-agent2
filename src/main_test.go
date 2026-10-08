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

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// main exits the process, so each case runs it in a copy of the test binary.
// KEA_PLUGIN_MAIN_ARGS holds the arguments passed after the program name.
func TestMain_Flags(t *testing.T) {
	t.Parallel()

	if args, ok := os.LookupEnv("KEA_PLUGIN_MAIN_ARGS"); ok {
		os.Args = append([]string{"zabbix-agent2-plugin-keadhcp"}, strings.Fields(args)...)

		main()

		return
	}

	cases := []struct {
		name, args string
		ok         bool
		want       string
	}{
		{"no arguments", "", true, "Usage of"},
		{"help", "-h", true, "Display this help message"},
		{"version", "-V", true, "Version 0.1.0"},
		{"unknown flag", "-x", false, "flag provided but not defined"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			//nolint:gosec // reruns this test binary
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMain_Flags$")

			cmd.Env = append(os.Environ(), "KEA_PLUGIN_MAIN_ARGS="+c.args)

			out, err := cmd.CombinedOutput()
			if (err == nil) != c.ok {
				t.Fatalf("exit error = %v, output:\n%s", err, out)
			}

			if !strings.Contains(string(out), c.want) {
				t.Fatalf("output does not contain %q:\n%s", c.want, out)
			}
		})
	}
}
