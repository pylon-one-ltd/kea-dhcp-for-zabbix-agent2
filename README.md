# Zabbix Agent 2 Plugin: Kea DHCP

This repository implements monitoring of ISC-Kea DHCP4 and DHCP6 servers into Zabbix via the use of a zabbix-agent2 plugin. The plugin facilitates access to the live statistic and reporting data in Kea via its control socket.

## Prerequisites

* **Go Environment**: Go 1.21+ is required to build the plugin (newer SDK branches may require a newer Go; the toolchain is fetched automatically).
* **Zabbix Agent 2**: An agent using plugin communication protocol 6.4.x (agent 6.4 and later, including 7.x) or 6.0.x (older agents). The plugin protocol version is not the agent version: run `zabbix_agent2 --version` and check the `Plugin communication protocol version` line. (The accompanying templates are designed for Zabbix Server 7.0+.)
* **Kea DHCP**: Must have the command API (control socket) enabled in your Kea configurations.

## Downloading a Release

Prebuild binaries and debian package files are included with each release. I endeavour to build against the current and 1 previous plugin support versions where possible unless major API changes make this impractical.

## Compatibility Matrix

Zabbix agent 2 only loads a plugin built for the same plugin communication protocol, and the protocol version is not the agent version. Check yours with `zabbix_agent2 --version` and look at the `Plugin communication protocol version` line, then pick the build for that protocol from the [releases page](https://github.com/pylon-one-ltd/kea-dhcp-for-zabbix-agent2/releases).

| Zabbix agent 2 | Plugin protocol | plugin-support branch | Release files |
|---|---|---|---|
| 6.4, 7.0, 7.2, 7.4 | 6.4.0 | `release/6.4` | `*-protocol6.4.0-linux-<arch>`, and the stable `zabbix-agent2-plugin-kea-linux-<arch>.deb` |
| 6.0 | 6.0.13 | `release/6.0` | `*-protocol6.0.13-linux-<arch>` |

`<arch>` is `amd64` or `arm64`. Each release has a plain binary (`zabbix-kea-plugin-vX.Y.Z-protocol...`) and a Debian package (`zabbix-agent2-plugin-kea-vX.Y.Z-protocol....deb`) for each row that is built. The protocol versions above are the ones each plugin-support branch declares; the `release/7.0`, `release/7.2` and `release/7.4` branches also speak 6.4.0, so the 6.4 build covers those agents.

## Building from Source

To compile the plugin, use a Zabbix plugin SDK branch that speaks your agent's plugin communication protocol, to avoid protocol mismatches that can hang the agent during startup. SDK branch names don't map to agent versions: `release/6.4` speaks protocol 6.4.0, which agent 6.4 and later (including 7.x) use. Check the `Plugin communication protocol version` line from `zabbix_agent2 --version`. The steps below build for protocol 6.4.0; the release workflow also shows how the protocol 6.0.x build is produced from `release/6.0`.

1. **Clone this repository and navigate into the source folder:**

   ```
   git clone git@github.com:pylon-one-ltd/kea-dhcp-for-zabbix-agent2.git zabbix-kea-plugin
   cd zabbix-kea-plugin/src
   ```

2. **Clone the Zabbix plugin SDK branch for protocol 6.4.0:**

   ```
   git clone -b release/6.4 https://git.zabbix.com/scm/ap/plugin-support.git ../plugin-support
   ```

3. **Point the module at your local SDK clone:**

   ```
   go mod edit -replace golang.zabbix.com/sdk=../plugin-support
   go mod tidy
   ```

4. **Run the tests and build the plugin:**

   ```
   go test ./...
   go build -o zabbix-kea-plugin .
   ```

5. **Lint (optional):** the code follows the Zabbix plugin style, checked with [golangci-lint](https://golangci-lint.run)

   ```
   golangci-lint fmt ./...
   golangci-lint run ./...
   ```

## Installation & Configuration (Manual)

1. **Move the compiled binary to your Zabbix plugins directory:**

   ```
   sudo mkdir -p /usr/libexec/zabbix
   sudo cp zabbix-kea-plugin /usr/libexec/zabbix/
   sudo chmod +x /usr/libexec/zabbix/zabbix-kea-plugin
   ```

2. **Configure Zabbix Agent 2:**
   Edit your Zabbix Agent 2 configuration file (usually `/etc/zabbix/zabbix_agent2.conf`) and add the path to the plugin, plus one named session per Kea daemon you want to monitor:

   ```
   Plugins.KeaDHCP.System.Path=/usr/libexec/zabbix/zabbix-kea-plugin
   Plugins.KeaDHCP.Sessions.dhcp4.Uri=unix:///run/kea/kea4-ctrl-socket
   Plugins.KeaDHCP.Sessions.dhcp6.Uri=unix:///run/kea/kea6-ctrl-socket
   ```

   A session URI can be `unix:///path/to/socket` (a bare absolute path also works) or an `http://` / `https://` URL for the Kea Control Agent. The plugin only connects to sessions defined here; item keys refer to them by name and cannot supply their own socket path or URL.

   Optionally, `Plugins.KeaDHCP.Timeout=<1-30>` sets the request timeout in seconds. It defaults to the agent's global `Timeout`.

   *Note: To avoid permission issues with the plugin socket during agent restarts, it is highly recommended to also set `PluginSocket=/run/zabbix/agent.plugin.sock` in your configuration.*

3. **Restart the Zabbix Agent 2 service:**

   ```
   sudo systemctl restart zabbix-agent2
   ```
## Installation & Configuration (Automated)

A debian .deb file is provided for installation using the package manager for debian based systems. This can be installed using the dpkg command line as below:

   ```
   wget https://github.com/pylon-one-ltd/kea-dhcp-for-zabbix-agent2/releases/latest/download/zabbix-agent2-plugin-kea-linux-amd64.deb
   sudo dpkg --install zabbix-agent2-plugin-kea-linux-amd64.deb
   rm zabbix-agent2-plugin-kea-linux-amd64.deb
   ```

For arm64, replace `amd64` with `arm64`. This URL always fetches the newest release's package for the latest communication version being targetted.
For older versions, see the compatability matrix and download the correct binary directly from the releases page.

## Zabbix Template Configuration

1. Log into your Zabbix Frontend.
2. Navigate to **Data Collection** -> **Templates**.
3. Click **Import** in the upper right corner and upload the `zabbix_kea_templates.yaml` file. This single file will import two modular templates:
   * **Kea DHCP4 by Zabbix agent 2**
   * **Kea DHCP6 by Zabbix agent 2**
4. Assign the appropriate templates to the host running your Kea server.

### Item Key Usage

The plugin exposes two keys. `<session>` is the name of a session defined in the agent configuration.

* `kea.stats[<session>, <command>, <service>]` returns the raw JSON answer to a read-only Kea command. Allowed commands are `statistic-get-all` (default), `statistic-get`, `status-get`, `version-get`, `build-get` and `ha-status-get`.
* `kea.subnets[<session>, <service>]` returns the configured subnets of `dhcp4` or `dhcp6` as `[{"id", "subnet", "name", "shared_network"}]`, including subnets inside shared networks. `name` is the subnet's `user-context` name when set, otherwise its prefix.

Examples:
* `kea.stats[dhcp4, statistic-get-all, dhcp4]`
* `kea.subnets[dhcp6, dhcp6]`

### Disclaimer

This repository has utilised generative AI models for the assistance of writing unit tests and documentation.
(Because I really hate writing unit tests. I'm sorry!)