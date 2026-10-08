/*
** Copyright (C) 2001-2026 Zabbix SIA
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
	"reflect"
	"strings"
	"testing"

	"golang.zabbix.com/sdk/log"
	"golang.zabbix.com/sdk/plugin"
)

func Test_KeaPlugin_Configure(t *testing.T) {
	t.Parallel()

	type fields struct {
		config *pluginConfig
	}

	type args struct {
		global  *plugin.GlobalOptions
		options any
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		want   *pluginConfig
	}{
		{
			"+valid",
			fields{},
			args{
				&plugin.GlobalOptions{Timeout: 3},
				[]byte(`Timeout=30`),
			},
			&pluginConfig{
				Timeout: 30,
			},
		},
		{
			"+prevConfig",
			fields{
				&pluginConfig{
					Timeout: 44,
				},
			},
			args{
				&plugin.GlobalOptions{Timeout: 3},
				[]byte(`Timeout=30`),
			},
			&pluginConfig{
				Timeout: 30,
			},
		},
		{
			"+globalOverwrite",
			fields{},
			args{
				&plugin.GlobalOptions{Timeout: 30},
				[]byte(``),
			},
			&pluginConfig{
				Timeout: 30,
			},
		},
		{
			"-marshalErr",
			fields{},
			args{
				&plugin.GlobalOptions{Timeout: 3},
				[]byte(
					strings.Join(
						[]string{"Timeout=2", "invalid"},
						"\n",
					),
				),
			},
			&pluginConfig{
				Timeout: 3,
			},
		},
		{
			"-marshalErrKeepsPrevious",
			fields{
				&pluginConfig{
					Timeout: 44,
				},
			},
			args{
				&plugin.GlobalOptions{Timeout: 3},
				[]byte("Timeout=2\ninvalid"),
			},
			&pluginConfig{
				Timeout: 44,
			},
		},
		{
			"+sessions",
			fields{},
			args{
				&plugin.GlobalOptions{Timeout: 3},
				[]byte("Sessions.dhcp4.Uri=unix:///run/kea/kea4-ctrl-socket\n"),
			},
			&pluginConfig{
				Timeout:  3,
				Sessions: map[string]session{serviceDHCP4: {URI: kea4URI}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &KeaPlugin{
				config: tt.fields.config,
				Base: plugin.Base{
					Logger: log.New("test"),
				},
			}

			p.Configure(tt.args.global, tt.args.options)

			if !reflect.DeepEqual(tt.want, p.config) {
				t.Fatalf("KeaPlugin.Configure() = %+v, want %+v", p.config, tt.want)
			}
		})
	}
}

func Test_KeaPlugin_Validate(t *testing.T) {
	t.Parallel()

	type args struct {
		options any
	}

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			"+valid",
			args{
				[]byte(`Timeout=30`),
			},
			false,
		},
		{
			"-marshalErr",
			args{
				[]byte(
					strings.Join(
						[]string{"Timeout=2", "invalid"},
						"\n",
					),
				),
			},
			true,
		},
		{
			"+sessions",
			args{
				[]byte("Sessions.dhcp4.Uri=unix:///run/kea/kea4-ctrl-socket\n" +
					"Sessions.dhcp6.Uri=/run/kea/kea6-ctrl-socket\n"),
			},
			false,
		},
		{
			"-badSession",
			args{
				[]byte("Sessions.ca.Uri=http://127.0.0.1:8000/\n"),
			},
			true,
		},
		{
			"-emptySessionUri",
			args{
				[]byte("Sessions.dhcp4.Uri=\n"),
			},
			true,
		},
		{
			"-timeoutOutOfRange",
			args{
				[]byte("Timeout=31"),
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := &KeaPlugin{}

			err := e.Validate(tt.args.options)
			if (err != nil) != tt.wantErr {
				t.Fatalf("KeaPlugin.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
