module github.com/pylon-one-ltd/kea-dhcp-for-zabbix-agent2

go 1.25.12

replace golang.zabbix.com/sdk => ../plugin-support

require golang.zabbix.com/sdk v0.0.0-00010101000000-000000000000

require (
	github.com/Microsoft/go-winio v0.6.0 // indirect
	golang.org/x/mod v0.12.0 // indirect
	golang.org/x/sys v0.10.0 // indirect
	golang.org/x/tools v0.11.0 // indirect
)
