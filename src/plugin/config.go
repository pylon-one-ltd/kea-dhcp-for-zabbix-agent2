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
	"net/url"
	"strings"

	"golang.zabbix.com/sdk/conf"
	"golang.zabbix.com/sdk/errs"
	"golang.zabbix.com/sdk/plugin"
)

type session struct {
	URI string `conf:"name=Uri"`
}

type pluginConfig struct {
	System plugin.SystemOptions `conf:"optional"`
	// Sessions stores pre-defined named sets of connections settings.
	Sessions map[string]session `conf:"optional"`
	// Comm timeout value.
	Timeout int `conf:"optional,range=1:30"`
}

// Configure implements the Configurator interface.
// Initializes configuration structures.
func (p *KeaPlugin) Configure(global *plugin.GlobalOptions, options any) {
	pConfig := &pluginConfig{}

	err := conf.Unmarshal(options, pConfig)
	if err != nil {
		p.Errf("cannot unmarshal configuration options: %s", err.Error())

		if p.config != nil {
			return
		}

		pConfig = &pluginConfig{}
	}

	p.config = pConfig
	if p.config.Timeout == 0 {
		p.config.Timeout = global.Timeout
	}
}

// Validate implements the Configurator interface.
// Returns an error if validation of a plugin's configuration is failed.
func (*KeaPlugin) Validate(options any) error {
	var opts pluginConfig

	err := conf.Unmarshal(options, &opts)
	if err != nil {
		return errs.Wrap(err, "failed to unmarshal configuration options")
	}

	for name, s := range opts.Sessions {
		_, _, err = parseURI(s.URI)
		if err != nil {
			return errs.Wrapf(
				err,
				"Plugins.%s.Sessions.%s.Uri",
				Name,
				name,
			)
		}
	}

	return nil
}

// parseURI validates a session URI from the config file and returns its
// network and address. For now it only supports unix sockets.
func parseURI(uri string) (string, string, error) {
	switch {
	case uri == "":
		return "", "", errs.New("URI is empty")
	case strings.HasPrefix(uri, "/"):
		return networkUnix, uri, nil
	}

	u, err := url.Parse(uri)
	if err != nil {
		return "", "", errs.Wrapf(err, "invalid URI %q", uri)
	}

	switch u.Scheme {
	case networkUnix:
		if u.Path == "" || u.Host != "" {
			return "", "", errs.Errorf("invalid unix URI %q: expected unix:///path/to/socket", uri)
		}

		return networkUnix, u.Path, nil
	// HTTP sessions are disabled for now. To enable them:
	//
	//	case "http", "https":
	//		if u.Host == "" {
	//			return "", "", errs.Errorf("invalid URI %q: missing host", uri)
	//		}
	//
	//		return networkHTTP, uri, nil
	default:
		return "", "", errs.Errorf("unsupported URI scheme %q: use unix, http or https", u.Scheme)
	}
}
