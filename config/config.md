# AvalancheGo Configs and Flags

This document lists all available configuration options for AvalancheGo nodes. You can configure your node using either command-line flags or environment variables.

> **Note:** For comparison with the previous documentation format (using individual flag headings), see the [archived version](https://gist.github.com/navillanueva/cdb9c49c411bd89a9480f05a7afbab37).

<style>{`
.config-tables {
  overflow-x: auto;
}
.config-tables table {
  min-width: 1400px;
}
.config-tables td:nth-child(5) {
  width: 50%;
}
.config-tables td:nth-child(-n+4) {
  white-space: nowrap;
}
`}</style>

## Environment Variable Naming Convention

All environment variables follow the pattern: `AVAGO_` + flag name where the flag name is converted to uppercase with hyphens replaced by underscores.

For example:
- Flag: `--api-admin-enabled`
- Environment Variable: `AVAGO_API_ADMIN_ENABLED`

## Example Usage

### Using Command-Line Flags

```bash
avalanchego --network-id=fuji --http-host=0.0.0.0 --log-level=debug
```

### Using Environment Variables

```bash
export AVAGO_NETWORK_ID=fuji
export AVAGO_HTTP_HOST=0.0.0.0
export AVAGO_LOG_LEVEL=debug
avalanchego
```

### Using Config File

Create a JSON config file:

```json
{
  "network-id": "fuji",
  "http-host": "0.0.0.0",
  "log-level": "debug"
}
```

Run with:

```bash
avalanchego --config-file=/path/to/config.json
```

## Configuration Precedence

Configuration sources are applied in the following order (highest to lowest precedence):

1. Command-line flags
2. Environment variables
3. Config file
4. Default values

# Configuration Options

<div className="config-tables">

### APIs

Configuration for various APIs exposed by the node.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--api-admin-enabled` | `AVAGO_API_ADMIN_ENABLED` | bool | `false` | If set to `true`, this node will expose the Admin API. See [here](https://build.avax.network/docs/api-reference/admin-api) for more information. |
| `--api-health-enabled` | `AVAGO_API_HEALTH_ENABLED` | bool | `true` | If set to `false`, this node will not expose the Health API. See [here](https://build.avax.network/docs/api-reference/health-api) for more information. |
| `--index-enabled` | `AVAGO_INDEX_ENABLED` | bool | `false` | If set to `true`, this node will enable the indexer and the Index API will be available. See [here](https://build.avax.network/docs/api-reference/index-api) for more information. |
| `--api-info-enabled` | `AVAGO_API_INFO_ENABLED` | bool | `true` | If set to `false`, this node will not expose the Info API. See [here](https://build.avax.network/docs/api-reference/info-api) for more information. |
| `--api-metrics-enabled` | `AVAGO_API_METRICS_ENABLED` | bool | `true` | If set to `false`, this node will not expose the Metrics API. See [here](https://build.avax.network/docs/api-reference/metrics-api) for more information. |

### Avalanche Community Proposals

Support for [Avalanche Community Proposals](https://github.com/avalanche-foundation/ACPs).

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--acp-support` | `AVAGO_ACP_SUPPORT` | []int | `[]` | The `--acp-support` flag allows an AvalancheGo node to indicate support for a set of [Avalanche Community Proposals](https://github.com/avalanche-foundation/ACPs). |
| `--acp-object` | `AVAGO_ACP_OBJECT` | []int | `[]` | The `--acp-object` flag allows an AvalancheGo node to indicate objection for a set of [Avalanche Community Proposals](https://github.com/avalanche-foundation/ACPs). |

### Bootstrapping

Configuration for node bootstrapping process.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--bootstrap-ancestors-max-containers-sent` | `AVAGO_BOOTSTRAP_ANCESTORS_MAX_CONTAINERS_SENT` | uint | `2000` | Max number of containers in an `Ancestors` message sent by this node. |
| `--bootstrap-ancestors-max-containers-received` | `AVAGO_BOOTSTRAP_ANCESTORS_MAX_CONTAINERS_RECEIVED` | uint | `2000` | This node reads at most this many containers from an incoming `Ancestors` message. |
| `--bootstrap-beacon-connection-timeout` | `AVAGO_BOOTSTRAP_BEACON_CONNECTION_TIMEOUT` | duration | `1m` | Timeout when attempting to connect to bootstrapping beacons. |
| `--bootstrap-ids` | `AVAGO_BOOTSTRAP_IDS` | string | network dependent | Bootstrap IDs is a comma-separated list of validator IDs. These IDs will be used to authenticate bootstrapping peers. An example setting of this field would be `--bootstrap-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg,NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ"`. The number of given IDs here must be same with number of given `--bootstrap-ips`. The default value depends on the network ID. |
| `--bootstrap-ips` | `AVAGO_BOOTSTRAP_IPS` | string | network dependent | Bootstrap IPs is a comma-separated list of IP:port pairs. These IP Addresses will be used to bootstrap the current Avalanche state. An example setting of this field would be `--bootstrap-ips="127.0.0.1:12345,1.2.3.4:5678"`. The number of given IPs here must be same with number of given `--bootstrap-ids`. The default value depends on the network ID. |
| `--bootstrap-max-time-get-ancestors` | `AVAGO_BOOTSTRAP_MAX_TIME_GET_ANCESTORS` | duration | `50ms` | Max Time to spend fetching a container and its ancestors when responding to a GetAncestors message. |
| `--bootstrap-retry-enabled` | `AVAGO_BOOTSTRAP_RETRY_ENABLED` | bool | `true` | If set to `false`, will not retry bootstrapping if it fails. |
| `--bootstrap-retry-warn-frequency` | `AVAGO_BOOTSTRAP_RETRY_WARN_FREQUENCY` | uint | `50` | Specifies how many times bootstrap should be retried before warning the operator. |

### Chain Configuration

Some blockchains allow the node operator to provide custom configurations for individual blockchains. These custom configurations are broken down into two categories: network upgrades and optional chain configurations. AvalancheGo reads in these configurations from the chain configuration directory and passes them into the VM on initialization.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--chain-config-dir` | `AVAGO_CHAIN_CONFIG_DIR` | string | `$HOME/.avalanchego/configs/chains` | Specifies the directory that contains chain configs, as described [here](https://build.avax.network/docs/nodes/chain-configs). If this flag is not provided and the default directory does not exist, AvalancheGo will not exit since custom configs are optional. However, if the flag is set, the specified folder must exist, or AvalancheGo will exit with an error. This flag is ignored if `--chain-config-content` is specified. Network upgrades are passed in from the location: `chain-config-dir`/`blockchainID`/`upgrade.*`. The chain configs are passed in from the location `chain-config-dir`/`blockchainID`/`config.*`. See [here](https://build.avax.network/docs/nodes/chain-configs) for more information. |
| `--chain-config-content` | `AVAGO_CHAIN_CONFIG_CONTENT` | string | - | As an alternative to `--chain-config-dir`, chains custom configurations can be loaded altogether from command line via `--chain-config-content` flag. Content must be base64 encoded. Example: First, encode the chain config: `echo -n '{"log-level":"trace"}' \| base64`. This will output something like `eyJsb2ctbGV2ZWwiOiJ0cmFjZSJ9`. Then create the full config JSON and encode it: `echo -n '{"C":{"Config":"eyJsb2ctbGV2ZWwiOiJ0cmFjZSJ9","Upgrade":null}}' \| base64`. Finally run: `avalanchego --chain-config-content "eyJDIjp7IkNvbmZpZyI6ImV5SnNiMmN0YkdWMlpXd2lPaUowY21GalpTSjkiLCJVcGdyYWRlIjpudWxsfX0="` |
| `--chain-aliases-file` | `AVAGO_CHAIN_ALIASES_FILE` | string | `~/.avalanchego/configs/chains/aliases.json` | Path to JSON file that defines aliases for Blockchain IDs. This flag is ignored if `--chain-aliases-file-content` is specified. Example content: `{"q2aTwKuyzgs8pynF7UXBZCU7DejbZbZ6EUyHr3JQzYgwNPUPi": ["DFK"]}`. The above example aliases the Blockchain whose ID is `"q2aTwKuyzgs8pynF7UXBZCU7DejbZbZ6EUyHr3JQzYgwNPUPi"` to `"DFK"`. Chain aliases are added after adding primary network aliases and before any changes to the aliases via the admin API. This means that the first alias included for a Blockchain on a Subnet will be treated as the `"Primary Alias"` instead of the full blockchainID. The Primary Alias is used in all metrics and logs. |
| `--chain-aliases-file-content` | `AVAGO_CHAIN_ALIASES_FILE_CONTENT` | string | - | As an alternative to `--chain-aliases-file`, it allows specifying base64 encoded aliases for Blockchains. |
| `--chain-data-dir` | `AVAGO_CHAIN_DATA_DIR` | string | `$HOME/.avalanchego/chainData` | Chain specific data directory. |

### Config File

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
|`--config-file` | `AVAGO_CONFIG_FILE` | string | - | Path to a JSON file that specifies this node's configuration. Command line arguments will override arguments set in the config file. This flag is ignored if `--config-file-content` is specified. Example JSON config file: `{"log-level": "debug"}`. [Install Script](https://build.avax.network/docs/tooling/avalanche-go-installer) creates the node config file at `~/.avalanchego/configs/node.json`. No default file is created if [AvalancheGo is built from source](https://build.avax.network/docs/nodes/run-a-node/from-source), you would need to create it manually if needed. |
| `--config-file-content` | `AVAGO_CONFIG_FILE_CONTENT` | string | - | As an alternative to `--config-file`, it allows specifying base64 encoded config content. |
| `--config-file-content-type` | `AVAGO_CONFIG_FILE_CONTENT_TYPE` | string | `JSON` | Specifies the format of the base64 encoded config content. JSON, TOML, YAML are among currently supported file format (see [here](https://github.com/spf13/viper#reading-config-files) for full list). |

### Data Directory

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--data-dir` | `AVAGO_DATA_DIR` | string | `$HOME/.avalanchego` | Sets the base data directory where default sub-directories will be placed unless otherwise specified. |

### Database

| Flag | Env Var | Type | Default  | Description |
|--------|--------|------|----|--------------------|
| `--db-dir` | `AVAGO_DB_DIR` | string | `$HOME/.avalanchego/db` | Specifies the directory to which the database is persisted. |
| `--db-type` | `AVAGO_DB_TYPE` | string | `leveldb` | Specifies the type of database to use. Must be one of `leveldb`, `memdb`, or `pebbledb`. `memdb` is an in-memory, non-persisted database. Note: `memdb` stores everything in memory. So if you have a 900 GiB LevelDB instance, then using `memdb` you'd need 900 GiB of RAM. `memdb` is useful for fast one-off testing, not for running an actual node (on Fuji or Mainnet). Also note that `memdb` doesn't persist after restart. So any time you restart the node it would start syncing from scratch. |

#### Database Config

| Flag | Env Var | Type | Default  | Description |
|--------|--------|------|----|--------------------|
| `--db-config-file` | `AVAGO_DB_CONFIG_FILE` | string | - | Path to the database config file. Ignored if `--db-config-file-content` is specified. |
| `--db-config-file-content` | `AVAGO_DB_CONFIG_FILE_CONTENT` | string | - | As an alternative to `--db-config-file`, it allows specifying base64 encoded database config content. |

A LevelDB config file must be JSON and may have these keys. Any keys not given will receive the default value. See [here](https://pkg.go.dev/github.com/syndtr/goleveldb/leveldb/opt#Options) for more information.

### File Descriptor Limit

| Flag | Env Var | Type | Default  | Description |
|--------|--------|------|----|--------------------|
| `--fd-limit` | `AVAGO_FD_LIMIT` | int | `32768` | Attempts to raise the process file descriptor limit to at least this value and error if the value is above the system max. |

### Genesis

| Flag | Env Var | Type | Default  | Description |
|--------|--------|------|----|--------------------|
| `--genesis-file` | `AVAGO_GENESIS_FILE` | string | - | Path to a JSON file containing the genesis data to use. Ignored when running standard networks (Mainnet, Fuji Testnet), or when `--genesis-file-content` is specified. If not given, uses default genesis data. See the documentation for the genesis JSON format [here](../genesis/README.md) and an example for a local network [here](../genesis/genesis_local.json). |
| `--genesis-file-content` | `AVAGO_GENESIS_FILE_CONTENT` | string | - | As an alternative to `--genesis-file`, it allows specifying base64 encoded genesis data to use. |

### HTTP Server

| Flag | Env Var | Type | Default  | Description |
|--------|--------|------|----|--------------------|
| `--http-allowed-hosts` | `AVAGO_HTTP_ALLOWED_HOSTS` | string | `localhost` | List of acceptable host names in API requests. Provide the wildcard (`'*'`) to accept requests from all hosts. API requests where the `Host` field is empty or an IP address will always be accepted. An API call whose HTTP `Host` field isn't acceptable will receive a 403 error code. |
| `--http-allowed-origins` | `AVAGO_HTTP_ALLOWED_ORIGINS` | string | `*` | Origins to allow on the HTTP port. Example: `"https://*.avax.network https://*.avax-test.network"` |
| `--http-host` | `AVAGO_HTTP_HOST` | string | `127.0.0.1` | The address that HTTP APIs listen on. This means that by default, your node can only handle API calls made from the same machine. To allow API calls from other machines, use `--http-host=`. You can also enter domain names as parameter. |
| `--http-port` | `AVAGO_HTTP_PORT` | int | `9650` | Each node runs an HTTP server that provides the APIs for interacting with the node and the Avalanche network. This argument specifies the port that the HTTP server will listen on. |
| `--http-idle-timeout` | `AVAGO_HTTP_IDLE_TIMEOUT` | duration | `120s` | Maximum duration to wait for the next request when keep-alives are enabled. If `--http-idle-timeout` is zero, the value of `--http-read-timeout` is used. If both are zero, there is no timeout. |
| `--http-read-timeout` | `AVAGO_HTTP_READ_TIMEOUT` | duration | `30s` | Maximum duration for reading the entire request, including the body. A zero or negative value means there will be no timeout. |
| `--http-read-header-timeout` | `AVAGO_HTTP_READ_HEADER_TIMEOUT` | duration | `30s` | Maximum duration to read request headers. The connection's read deadline is reset after reading the headers. If `--http-read-header-timeout` is zero, the value of `--http-read-timeout` is used. If both are zero, there is no timeout. |
| `--http-write-timeout` | `AVAGO_HTTP_WRITE_TIMEOUT` | duration | `30s` | Maximum duration before timing out writes of the response. It is reset whenever a new request's header is read. A zero or negative value means there will be no timeout. |
| `--http-shutdown-timeout` | `AVAGO_HTTP_SHUTDOWN_TIMEOUT` | duration | `10s` | Maximum duration to wait for existing connections to complete during node shutdown. |
| `--http-shutdown-wait` | `AVAGO_HTTP_SHUTDOWN_WAIT` | duration | `0s` | Duration to wait after receiving SIGTERM or SIGINT before initiating shutdown. The `/health` endpoint will return unhealthy during this duration (if the Health API is enabled.) |
| `--http-tls-enabled` | `AVAGO_HTTP_TLS_ENABLED` | boolean | `false` | If set to `true`, this flag will attempt to upgrade the server to use HTTPS. |
| `--http-tls-cert-file` | `AVAGO_HTTP_TLS_CERT_FILE` | string | - | This argument specifies the location of the TLS certificate used by the node for the HTTPS server. This must be specified when `--http-tls-enabled=true`. There is no default value. This flag is ignored if `--http-tls-cert-file-content` is specified. |
| `--http-tls-cert-file-content` | `AVAGO_HTTP_TLS_CERT_FILE_CONTENT` | string | - | As an alternative to `--http-tls-cert-file`, it allows specifying base64 encoded content of the TLS certificate used by the node for the HTTPS server. Note that full certificate content, with the leading and trailing header, must be base64 encoded. This must be specified when `--http-tls-enabled=true`. |
| `--http-tls-key-file` | `AVAGO_HTTP_TLS_KEY_FILE` | string | - | This argument specifies the location of the TLS private key used by the node for the HTTPS server. This must be specified when `--http-tls-enabled=true`. There is no default value. This flag is ignored if `--http-tls-key-file-content` is specified. |
| `--http-tls-key-file-content` | `AVAGO_HTTP_TLS_KEY_FILE_CONTENT` | string | - | As an alternative to `--http-tls-key-file`, it allows specifying base64 encoded content of the TLS private key used by the node for the HTTPS server. Note that full private key content, with the leading and trailing header, must be base64 encoded. This must be specified when `--http-tls-enabled=true`. |

### Logging

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--log-level=off` | `AVAGO_LOG_LEVEL` | string | `info` | No logs. |
| `--log-level=fatal` | `AVAGO_LOG_LEVEL` | string | `info` | Fatal errors that are not recoverable. |
| `--log-level=error` | `AVAGO_LOG_LEVEL` | string | `info` | Errors that the node encounters, these errors were able to be recovered. |
| `--log-level=warn` | `AVAGO_LOG_LEVEL` | string | `info` | Warnings that might be indicative of a spurious byzantine node, or potential future error. |
| `--log-level=info` | `AVAGO_LOG_LEVEL` | string | `info` | Useful descriptions of node status updates. |
| `--log-level=trace` | `AVAGO_LOG_LEVEL` | string | `info` | Traces container job results, useful for tracing container IDs and their outcomes. |
| `--log-level=debug` | `AVAGO_LOG_LEVEL` | string | `info` | Useful when attempting to understand possible bugs in the code. |
| `--log-level=verbo` | `AVAGO_LOG_LEVEL` | string | `info` | Tracks extensive amounts of information the node is processing, including message contents and binary dumps of data for extremely low level protocol analysis. |
| `--log-display-level` | `AVAGO_LOG_DISPLAY_LEVEL` | string | value of `--log-level` | The log level determines which events to display to stdout. If left blank, will default to the value provided to `--log-level`. |
| `--log-format=auto` | `AVAGO_LOG_FORMAT` | string | `auto` | Formats terminal-like logs when the output is a terminal. |
| `--log-format=plain` | `AVAGO_LOG_FORMAT` | string | `auto` | Plain text log format. |
| `--log-format=colors` | `AVAGO_LOG_FORMAT` | string | `auto` | Colored log format. |
| `--log-format=json` | `AVAGO_LOG_FORMAT` | string | `auto` | JSON log format. |
| `--log-dir` | `AVAGO_LOG_DIR` | string | `$HOME/.avalanchego/logs` | Specifies the directory in which system logs are kept. If you are running the node as a system service (ex. using the installer script) logs will also be stored in `$HOME/var/log/syslog`. |
| `--log-disable-display-plugin-logs` | `AVAGO_LOG_DISABLE_DISPLAY_PLUGIN_LOGS` | boolean | `false` | Disables displaying plugin logs in stdout. |
| `--log-rotater-max-size` | `AVAGO_LOG_ROTATER_MAX_SIZE` | uint | `8` | The maximum file size in megabytes of the log file before it gets rotated. |
| `--log-rotater-max-files` | `AVAGO_LOG_ROTATER_MAX_FILES` | uint | `7` | The maximum number of old log files to retain. 0 means retain all old log files. |
| `--log-rotater-max-age` | `AVAGO_LOG_ROTATER_MAX_AGE` | uint | `0` | The maximum number of days to retain old log files based on the timestamp encoded in their filename. 0 means retain all old log files. |
| `--log-rotater-compress-enabled` | `AVAGO_LOG_ROTATER_COMPRESS_ENABLED` | boolean | `false` | Enables the compression of rotated log files through gzip. |

### Continuous Profiling

You can configure your node to continuously run memory/CPU profiles and save the most recent ones. Continuous memory/CPU profiling is enabled if `--profile-continuous-enabled` is set.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--profile-continuous-enabled` | `AVAGO_PROFILE_CONTINUOUS_ENABLED` | boolean | `false` | Whether the app should continuously produce performance profiles. |
| `--profile-dir` | `AVAGO_PROFILE_DIR` | string | `$HOME/.avalanchego/profiles/` | If profiling is enabled, node continuously runs memory/CPU profiles and puts them at this directory. |
| `--profile-continuous-freq` | `AVAGO_PROFILE_CONTINUOUS_FREQ` | duration | `15m` | How often a new CPU/memory profile is created. |
| `--profile-continuous-max-files` | `AVAGO_PROFILE_CONTINUOUS_MAX_FILES` | int | `5` | Maximum number of CPU/memory profile files to keep. |

### Network

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--network-id=mainnet` | `AVAGO_NETWORK_ID` | string | `mainnet` | Connect to Mainnet (default). |
| `--network-id=fuji` | `AVAGO_NETWORK_ID` | string | `mainnet` | Connect to the Fuji test-network. |
| `--network-id=testnet` | `AVAGO_NETWORK_ID` | string | `mainnet` | Connect to the current test-network (currently Fuji). |
| `--network-id=local` | `AVAGO_NETWORK_ID` | string | `mainnet` | Connect to a local test-network. |
| `--network-id=network-[id]` | `AVAGO_NETWORK_ID` | string | `mainnet` | Connect to the network with the given ID. `id` must be in the range \[0, 2^32\). |

### OpenTelemetry

AvalancheGo supports collecting and exporting [OpenTelemetry](https://opentelemetry.io/) traces. This might be useful for debugging, performance analysis, or monitoring.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--tracing-endpoint` | `AVAGO_TRACING_ENDPOINT` | string | `localhost:4317` (gRPC) or `localhost:4318` (HTTP) | The endpoint to export trace data to. Default depends on `--tracing-exporter-type`. |
| `--tracing-exporter-type` | `AVAGO_TRACING_EXPORTER_TYPE` | string | `disabled` | Type of exporter to use for tracing. Options are \`disabled\`, \`grpc\`, \`http\`. |
| `--tracing-insecure` | `AVAGO_TRACING_INSECURE` | boolean | `true` | If true, don't use TLS when exporting trace data. |
| `--tracing-sample-rate` | `AVAGO_TRACING_SAMPLE_RATE` | float | `0.1` | The fraction of traces to sample. If \>= 1, always sample. If \<= 0, never sample. |

### Partial Sync Primary Network

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--partial-sync-primary-network` | `AVAGO_PARTIAL_SYNC_PRIMARY_NETWORK` | boolean | `false` | Partial sync enables nodes that are not primary network validators to optionally sync only the P-chain on the primary network. Nodes that use this option can still track Subnets. After the Etna upgrade, nodes that use this option can also validate L1s. |

### Public IP

Validators must know one of their public facing IP addresses so they can enable other nodes to connect to them. By default, the node will attempt to perform NAT traversal to get the node's IP according to its router.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--public-ip` | `AVAGO_PUBLIC_IP` | string | - | If this argument is provided, the node assumes this is its public IP. When running a local network it may be easiest to set this value to `127.0.0.1`. |
| `--public-ip-resolution-frequency` | `AVAGO_PUBLIC_IP_RESOLUTION_FREQUENCY` | duration | `5m` | Frequency at which this node resolves/updates its public IP and renew NAT mappings, if applicable. |
| `--public-ip-resolution-service` | `AVAGO_PUBLIC_IP_RESOLUTION_SERVICE` | string | - | When provided, the node will use that service to periodically resolve/update its public IP. Only acceptable values are `ifconfigCo`, `opendns` or `ifconfigMe`. |

### State Syncing

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--state-sync-ids` | `AVAGO_STATE_SYNC_IDS` | string | - | State sync IDs is a comma-separated list of validator IDs. The specified validators will be contacted to get and authenticate the starting point (state summary) for state sync. An example setting of this field would be `--state-sync-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg,NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ"`. The number of given IDs here must be same with number of given `--state-sync-ips`. The default value is empty, which results in all validators being sampled. |
| `--state-sync-ips` | `AVAGO_STATE_SYNC_IPS` | string | - | State sync IPs is a comma-separated list of IP:port pairs. These IP Addresses will be contacted to get and authenticate the starting point (state summary) for state sync. An example setting of this field would be `--state-sync-ips="127.0.0.1:12345,1.2.3.4:5678"`. The number of given IPs here must be the same with the number of given `--state-sync-ids`. |

### Staking

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--staking-port` | `AVAGO_STAKING_PORT` | int | `9651` | The port through which the network peers will connect to this node externally. Having this port accessible from the internet is required for correct node operation. |
| `--staking-tls-cert-file` | `AVAGO_STAKING_TLS_CERT_FILE` | string | `$HOME/.avalanchego/staking/staker.crt` | Avalanche uses two-way authenticated TLS connections to securely connect nodes. This argument specifies the location of the TLS certificate used by the node. This flag is ignored if `--staking-tls-cert-file-content` is specified. |
| `--staking-tls-cert-file-content` | `AVAGO_STAKING_TLS_CERT_FILE_CONTENT` | string | - | As an alternative to `--staking-tls-cert-file`, it allows specifying base64 encoded content of the TLS certificate used by the node. Note that full certificate content, with the leading and trailing header, must be base64 encoded. |
| `--staking-tls-key-file` | `AVAGO_STAKING_TLS_KEY_FILE` | string | `$HOME/.avalanchego/staking/staker.key` | Avalanche uses two-way authenticated TLS connections to securely connect nodes. This argument specifies the location of the TLS private key used by the node. This flag is ignored if `--staking-tls-key-file-content` is specified. |
| `--staking-tls-key-file-content` | `AVAGO_STAKING_TLS_KEY_FILE_CONTENT` | string | - | As an alternative to `--staking-tls-key-file`, it allows specifying base64 encoded content of the TLS private key used by the node. Note that full private key content, with the leading and trailing header, must be base64 encoded. |

### Subnets

#### Subnet Tracking

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--track-subnets` | `AVAGO_TRACK_SUBNETS` | string | - | Comma separated list of Subnet IDs that this node would track if added to. Defaults to empty (will only validate the Primary Network). |

#### Subnet Configs

It is possible to provide parameters for Subnets. Parameters here apply to all chains in the specified Subnets. Parameters must be specified with a `[subnetID].json` config file under `--subnet-config-dir`. AvalancheGo loads configs for Subnets specified in `--track-subnets` parameter. Full reference for all configuration options for a Subnet can be found in a separate [Subnet Configs](https://build.avax.network/docs/nodes/configure/avalanche-l1-configs) document.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--subnet-config-dir` | `AVAGO_SUBNET_CONFIG_DIR` | string | `$HOME/.avalanchego/configs/subnets` | Specifies the directory that contains Subnet configs, as described above. If the flag is set explicitly, the specified folder must exist, or AvalancheGo will exit with an error. This flag is ignored if `--subnet-config-content` is specified. Example: Let's say we have a Subnet with ID `p4jUwqZsA2LuSftroCd3zb4ytH8W99oXKuKVZdsty7eQ3rXD6`. We can create a config file under the default `subnet-config-dir` at `$HOME/.avalanchego/configs/subnets/p4jUwqZsA2LuSftroCd3zb4ytH8W99oXKuKVZdsty7eQ3rXD6.json`. An example config file is: `{"validatorOnly": false, "snowballParameters":{"k":25,"alpha":18}}`. By default, none of these directories and/or files exist. You would need to create them manually if needed. |
| `--subnet-config-content` | `AVAGO_SUBNET_CONFIG_CONTENT` | string | - | As an alternative to `--subnet-config-dir`, it allows specifying base64 encoded parameters for a Subnet. |

### Version

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--version` | `AVAGO_VERSION` | boolean | `false` | If this is `true`, print the version and quit. |

# Advanced Configuration Options

⚠️ **Warning**: The following options may affect the correctness of a node. Only power users should change these.

### Gossiping

Consensus gossiping parameters.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--consensus-accepted-frontier-gossip-validator-size` | `AVAGO_CONSENSUS_ACCEPTED_FRONTIER_GOSSIP_VALIDATOR_SIZE` | uint | `0` | Number of validators to gossip to when gossiping accepted frontier. |
| `--consensus-accepted-frontier-gossip-non-validator-size` | `AVAGO_CONSENSUS_ACCEPTED_FRONTIER_GOSSIP_NON_VALIDATOR_SIZE` | uint | `0` | Number of non-validators to gossip to when gossiping accepted frontier. |
| `--consensus-accepted-frontier-gossip-peer-size` | `AVAGO_CONSENSUS_ACCEPTED_FRONTIER_GOSSIP_PEER_SIZE` | uint | `15` | Number of peers to gossip to when gossiping accepted frontier. |
| `--consensus-accepted-frontier-gossip-frequency` | `AVAGO_CONSENSUS_ACCEPTED_FRONTIER_GOSSIP_FREQUENCY` | duration | `10s` | Time between gossiping accepted frontiers. |
| `--consensus-on-accept-gossip-validator-size` | `AVAGO_CONSENSUS_ON_ACCEPT_GOSSIP_VALIDATOR_SIZE` | uint | `0` | Number of validators to gossip to each accepted container to. |
| `--consensus-on-accept-gossip-non-validator-size` | `AVAGO_CONSENSUS_ON_ACCEPT_GOSSIP_NON_VALIDATOR_SIZE` | uint | `0` | Number of non-validators to gossip to each accepted container to. |
| `--consensus-on-accept-gossip-peer-size` | `AVAGO_CONSENSUS_ON_ACCEPT_GOSSIP_PEER_SIZE` | uint | `10` | Number of peers to gossip to each accepted container to. |

### Sybil Protection

Sybil protection configuration. These settings affect how the node participates in consensus.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--sybil-protection-enabled` | `AVAGO_SYBIL_PROTECTION_ENABLED` | boolean | `true` | Avalanche uses Proof of Stake (PoS) as sybil resistance to make it prohibitively expensive to attack the network. If false, sybil resistance is disabled and all peers will be sampled during consensus. Note that this can not be disabled on public networks (`Fuji` and `Mainnet`). Setting this flag to `false` **does not** mean "this node is not a validator." It means that this node will sample all nodes, not just validators. **You should not set this flag to false unless you understand what you are doing.** |
| `--sybil-protection-disabled-weight` | `AVAGO_SYBIL_PROTECTION_DISABLED_WEIGHT` | uint | `100` | Weight to provide to each peer when staking is disabled. |

### Benchlist

Peer benchlisting configuration.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--benchlist-duration` | `AVAGO_BENCHLIST_DURATION` | duration | `15m` | Maximum amount of time a peer is benchlisted after surpassing `--benchlist-fail-threshold`. |
| `--benchlist-fail-threshold` | `AVAGO_BENCHLIST_FAIL_THRESHOLD` | int | `10` | Number of consecutive failed queries to a node before benching it (assuming all queries to it will fail). |
| `--benchlist-min-failing-duration` | `AVAGO_BENCHLIST_MIN_FAILING_DURATION` | duration | `150s` | Minimum amount of time queries to a peer must be failing before the peer is benched. |

### Consensus Parameters

:::note
Some of these parameters can only be set on a local or private network, not on Fuji Testnet or Mainnet
:::

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--consensus-shutdown-timeout` | `AVAGO_CONSENSUS_SHUTDOWN_TIMEOUT` | duration | `5s` | Timeout before killing an unresponsive chain. |
| `--create-asset-tx-fee` | `AVAGO_CREATE_ASSET_TX_FEE` | int | `10000000` | Transaction fee, in nAVAX, for transactions that create new assets. This can only be changed on a local network. |
| `--tx-fee` | `AVAGO_TX_FEE` | int | `1000000` | The required amount of nAVAX to be burned for a transaction to be valid on the X-Chain, and for import/export transactions on the P-Chain. This parameter requires network agreement in its current form. Changing this value from the default should only be done on private networks or local network. |
| `--uptime-requirement` | `AVAGO_UPTIME_REQUIREMENT` | float | `0.8` | Fraction of time a validator must be online to receive rewards. This can only be changed on a local network. |
| `--uptime-metric-freq` | `AVAGO_UPTIME_METRIC_FREQ` | duration | `30s` | Frequency of renewing this node's average uptime metric. |

### Staking Parameters

Staking economics configuration.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--min-validator-stake` | `AVAGO_MIN_VALIDATOR_STAKE` | int | network dependent | The minimum stake, in nAVAX, required to validate the Primary Network. This can only be changed on a local network. Defaults to `2000000000000` (2,000 AVAX) on Mainnet. Defaults to `5000000` (.005 AVAX) on Test Net. |
| `--max-validator-stake` | `AVAGO_MAX_VALIDATOR_STAKE` | int | network dependent | The maximum stake, in nAVAX, that can be placed on a validator on the primary network. This includes stake provided by both the validator and by delegators to the validator. This can only be changed on a local network. |
| `--min-delegator-stake` | `AVAGO_MIN_DELEGATOR_STAKE` | int | network dependent | The minimum stake, in nAVAX, that can be delegated to a validator of the Primary Network. Defaults to `25000000000` (25 AVAX) on Mainnet. Defaults to `5000000` (.005 AVAX) on Test Net. This can only be changed on a local network. |
| `--min-delegation-fee` | `AVAGO_MIN_DELEGATION_FEE` | int | `20000` | The minimum delegation fee that can be charged for delegation on the Primary Network, multiplied by \`10,000\`. Must be in the range \[0, 1000000\]. This can only be changed on a local network. |
| `--min-stake-duration` | `AVAGO_MIN_STAKE_DURATION` | duration | `336h` | Minimum staking duration. This can only be changed on a local network. This applies to both delegation and validation periods. |
| `--max-stake-duration` | `AVAGO_MAX_STAKE_DURATION` | duration | `8760h` | The maximum staking duration, in hours. This can only be changed on a local network. |
| `--stake-minting-period` | `AVAGO_STAKE_MINTING_PERIOD` | duration | `8760h` | Consumption period of the staking function, in hours. This can only be changed on a local network. |
| `--stake-max-consumption-rate` | `AVAGO_STAKE_MAX_CONSUMPTION_RATE` | uint | `120000` | The maximum percentage of the consumption rate for the remaining token supply in the minting period, which is 1 year on Mainnet. This can only be changed on a local network. |
| `--stake-min-consumption-rate` | `AVAGO_STAKE_MIN_CONSUMPTION_RATE` | uint | `100000` | The minimum percentage of the consumption rate for the remaining token supply in the minting period, which is 1 year on Mainnet. This can only be changed on a local network. |
| `--stake-supply-cap` | `AVAGO_STAKE_SUPPLY_CAP` | uint | `720000000000000000` | The maximum stake supply, in nAVAX, that can be placed on a validator. This can only be changed on a local network. |

### Snow Consensus

Snow consensus protocol parameters.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--snow-concurrent-repolls` | `AVAGO_SNOW_CONCURRENT_REPOLLS` | int | `4` | Snow consensus requires repolling transactions that are issued during low time of network usage. This parameter lets one define how aggressive the client will be in finalizing these pending transactions. This should only be changed after careful consideration of the tradeoffs of Snow consensus. The value must be at least `1` and at most `--snow-commit-threshold`. |
| `--snow-sample-size` | `AVAGO_SNOW_SAMPLE_SIZE` | int | `20` | Snow consensus defines `k` as the number of validators that are sampled during each network poll. This parameter lets one define the `k` value used for consensus. This should only be changed after careful consideration of the tradeoffs of Snow consensus. The value must be at least `1`. |
| `--snow-quorum-size` | `AVAGO_SNOW_QUORUM_SIZE` | int | `15` | Snow consensus defines `alpha` as the number of validators that must prefer a transaction during each network poll to increase the confidence in the transaction. This parameter lets us define the `alpha` value used for consensus. This should only be changed after careful consideration of the tradeoffs of Snow consensus. The value must be at greater than `k/2`. |
| `--snow-commit-threshold` | `AVAGO_SNOW_COMMIT_THRESHOLD` | int | `20` | Snow consensus defines `beta` as the number of consecutive polls that a container must increase its confidence for it to be accepted. This parameter lets us define the `beta` value used for consensus. This should only be changed after careful consideration of the tradeoffs of Snow consensus. The value must be at least `1`. |
| `--snow-optimal-processing` | `AVAGO_SNOW_OPTIMAL_PROCESSING` | int | `50` | Optimal number of processing items in consensus. The value must be at least `1`. |
| `--snow-max-processing` | `AVAGO_SNOW_MAX_PROCESSING` | int | `1024` | Maximum number of processing items to be considered healthy. Reports unhealthy if more than this number of items are outstanding. The value must be at least `1`. |
| `--snow-max-time-processing` | `AVAGO_SNOW_MAX_TIME_PROCESSING` | duration | `2m` | Maximum amount of time an item should be processing and still be healthy. Reports unhealthy if there is an item processing for longer than this duration. The value must be greater than `0`. |

### ProposerVM

ProposerVM configuration.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--proposervm-use-current-height` | `AVAGO_PROPOSERVM_USE_CURRENT_HEIGHT` | boolean | `false` | Have the ProposerVM always report the last accepted P-chain block height. |
| `--proposervm-min-block-delay` | `AVAGO_PROPOSERVM_MIN_BLOCK_DELAY` | duration | `1s` | The minimum delay to enforce when building a snowman++ block for the primary network chains and the default minimum delay for subnets. A non-default value is only suggested for non-production nodes. |

### Health Checks

Health monitoring configuration.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--health-check-frequency` | `AVAGO_HEALTH_CHECK_FREQUENCY` | duration | `30s` | Health check runs with this frequency. |
| `--health-check-averager-halflife` | `AVAGO_HEALTH_CHECK_AVERAGER_HALFLIFE` | duration | `10s` | Half life of averagers used in health checks (to measure the rate of message failures, for example.) Larger value -> less volatile calculation of averages. |

### Network Configuration

Advanced network settings.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--network-allow-private-ips` | `AVAGO_NETWORK_ALLOW_PRIVATE_IPS` | boolean | `true` | Allows the node to connect peers with private IPs. |
| `--network-compression-type` | `AVAGO_NETWORK_COMPRESSION_TYPE` | string | `gzip` | The type of compression to use when sending messages to peers. Must be one of \`gzip\`, \`zstd\`, \`none\`. Nodes can handle inbound \`gzip\` compressed messages but by default send \`zstd\` compressed messages. |
| `--network-initial-timeout` | `AVAGO_NETWORK_INITIAL_TIMEOUT` | duration | `5s` | Initial timeout value of the adaptive timeout manager. |
| `--network-initial-reconnect-delay` | `AVAGO_NETWORK_INITIAL_RECONNECT_DELAY` | duration | `1s` | Initial delay duration must be waited before attempting to reconnect a peer. |
| `--network-max-reconnect-delay` | `AVAGO_NETWORK_MAX_RECONNECT_DELAY` | duration | `1h` | Maximum delay duration must be waited before attempting to reconnect a peer. |
| `--network-minimum-timeout` | `AVAGO_NETWORK_MINIMUM_TIMEOUT` | duration | `2s` | Minimum timeout value of the adaptive timeout manager. |
| `--network-maximum-timeout` | `AVAGO_NETWORK_MAXIMUM_TIMEOUT` | duration | `10s` | Maximum timeout value of the adaptive timeout manager. |
| `--network-maximum-inbound-timeout` | `AVAGO_NETWORK_MAXIMUM_INBOUND_TIMEOUT` | duration | `10s` | Maximum timeout value of an inbound message. Defines duration within which an incoming message must be fulfilled. Incoming messages containing deadline higher than this value will be overridden with this value. |
| `--network-timeout-halflife` | `AVAGO_NETWORK_TIMEOUT_HALFLIFE` | duration | `5m` | Half life used when calculating average network latency. Larger value -> less volatile network latency calculation. |
| `--network-timeout-coefficient` | `AVAGO_NETWORK_TIMEOUT_COEFFICIENT` | float | `2` | Requests to peers will time out after \[network-timeout-coefficient\] \* \[average request latency\]. |
| `--network-read-handshake-timeout` | `AVAGO_NETWORK_READ_HANDSHAKE_TIMEOUT` | duration | `15s` | Timeout value for reading handshake messages. |
| `--network-ping-timeout` | `AVAGO_NETWORK_PING_TIMEOUT` | duration | `30s` | Timeout value for Ping-Pong with a peer. |
| `--network-ping-frequency` | `AVAGO_NETWORK_PING_FREQUENCY` | duration | `22.5s` | Frequency of pinging other peers. |
| `--network-health-min-conn-peers` | `AVAGO_NETWORK_HEALTH_MIN_CONN_PEERS` | uint | `1` | Node will report unhealthy if connected to less than this many peers. |
| `--network-health-max-time-since-msg-received` | `AVAGO_NETWORK_HEALTH_MAX_TIME_SINCE_MSG_RECEIVED` | duration | `1m` | Node will report unhealthy if it hasn't received a message for this amount of time. |
| `--network-health-max-time-since-msg-sent` | `AVAGO_NETWORK_HEALTH_MAX_TIME_SINCE_MSG_SENT` | duration | `1m` | Network layer returns unhealthy if haven't sent a message for at least this much time. |
| `--network-health-max-portion-send-queue-full` | `AVAGO_NETWORK_HEALTH_MAX_PORTION_SEND_QUEUE_FULL` | float | `0.9` | Node will report unhealthy if its send queue is more than this portion full. Must be in \[0,1\]. |
| `--network-health-max-send-fail-rate` | `AVAGO_NETWORK_HEALTH_MAX_SEND_FAIL_RATE` | float | `0.25` | Node will report unhealthy if more than this portion of message sends fail. Must be in \[0,1\]. |
| `--network-health-max-outstanding-request-duration` | `AVAGO_NETWORK_HEALTH_MAX_OUTSTANDING_REQUEST_DURATION` | duration | `5m` | Node reports unhealthy if there has been a request outstanding for this duration. |
| `--network-max-clock-difference` | `AVAGO_NETWORK_MAX_CLOCK_DIFFERENCE` | duration | `1m` | Max allowed clock difference value between this node and peers. |
| `--network-require-validator-to-connect` | `AVAGO_NETWORK_REQUIRE_VALIDATOR_TO_CONNECT` | boolean | `false` | If true, this node will only maintain a connection with another node if this node is a validator, the other node is a validator, or the other node is a beacon. |
| `--network-tcp-proxy-enabled` | `AVAGO_NETWORK_TCP_PROXY_ENABLED` | boolean | `false` | Require all P2P connections to be initiated with a TCP proxy header. |
| `--network-tcp-proxy-read-timeout` | `AVAGO_NETWORK_TCP_PROXY_READ_TIMEOUT` | duration | `3s` | Maximum duration to wait for a TCP proxy header. |
| `--network-outbound-connection-timeout` | `AVAGO_NETWORK_OUTBOUND_CONNECTION_TIMEOUT` | duration | `30s` | Timeout while dialing a peer. |

### Message Rate-Limiting

These flags govern rate-limiting of inbound and outbound messages. For more information on rate-limiting and the flags below, see package `throttling` in AvalancheGo.

#### CPU Based Rate-Limiting

Rate-limiting based on how much CPU usage a peer causes.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--throttler-inbound-cpu-validator-alloc` | `AVAGO_THROTTLER_INBOUND_CPU_VALIDATOR_ALLOC` | float | half of CPUs | Number of CPU allocated for use by validators. Value should be in range \(0, total core count\]. |
| `--throttler-inbound-cpu-max-recheck-delay` | `AVAGO_THROTTLER_INBOUND_CPU_MAX_RECHECK_DELAY` | duration | `5s` | In the CPU rate-limiter, check at least this often whether the node's CPU usage has fallen to an acceptable level. |
| `--throttler-inbound-disk-max-recheck-delay` | `AVAGO_THROTTLER_INBOUND_DISK_MAX_RECHECK_DELAY` | duration | `5s` | In the disk-based network throttler, check at least this often whether the node's disk usage has fallen to an acceptable level. |
| `--throttler-inbound-cpu-max-non-validator-usage` | `AVAGO_THROTTLER_INBOUND_CPU_MAX_NON_VALIDATOR_USAGE` | float | 80% of CPUs | Number of CPUs that if fully utilized, will rate limit all non-validators. Value should be in range \[0, total core count\]. |
| `--throttler-inbound-cpu-max-non-validator-node-usage` | `AVAGO_THROTTLER_INBOUND_CPU_MAX_NON_VALIDATOR_NODE_USAGE` | float | CPUs / 8 | Maximum number of CPUs that a non-validator can utilize. Value should be in range \[0, total core count\]. |
| `--throttler-inbound-disk-validator-alloc` | `AVAGO_THROTTLER_INBOUND_DISK_VALIDATOR_ALLOC` | float | `1000 GiB/s` | Maximum number of disk reads/writes per second to allocate for use by validators. Must be > 0. |
| `--throttler-inbound-disk-max-non-validator-usage` | `AVAGO_THROTTLER_INBOUND_DISK_MAX_NON_VALIDATOR_USAGE` | float | `1000 GiB/s` | Number of disk reads/writes per second that, if fully utilized, will rate limit all non-validators. Must be \>= 0. |
| `--throttler-inbound-disk-max-non-validator-node-usage` | `AVAGO_THROTTLER_INBOUND_DISK_MAX_NON_VALIDATOR_NODE_USAGE` | float | `1000 GiB/s` | Maximum number of disk reads/writes per second that a non-validator can utilize. Must be \>= 0. |

#### Bandwidth Based Rate-Limiting

Rate-limiting based on the bandwidth a peer uses.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--throttler-inbound-bandwidth-refill-rate` | `AVAGO_THROTTLER_INBOUND_BANDWIDTH_REFILL_RATE` | uint | `512` | Max average inbound bandwidth usage of a peer, in bytes per second. See interface `throttling.BandwidthThrottler`. |
| `--throttler-inbound-bandwidth-max-burst-size` | `AVAGO_THROTTLER_INBOUND_BANDWIDTH_MAX_BURST_SIZE` | uint | `2 MiB` | Max inbound bandwidth a node can use at once. See interface `throttling.BandwidthThrottler`. |

#### Message Size Based Rate-Limiting

Rate-limiting based on the total size, in bytes, of unprocessed messages.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--throttler-inbound-at-large-alloc-size` | `AVAGO_THROTTLER_INBOUND_AT_LARGE_ALLOC_SIZE` | uint | `6 MiB` | Size, in bytes, of at-large allocation in the inbound message throttler. |
| `--throttler-inbound-validator-alloc-size` | `AVAGO_THROTTLER_INBOUND_VALIDATOR_ALLOC_SIZE` | uint | `32 MiB` | Size, in bytes, of validator allocation in the inbound message throttler. |
| `--throttler-inbound-node-max-at-large-bytes` | `AVAGO_THROTTLER_INBOUND_NODE_MAX_AT_LARGE_BYTES` | uint | `2 MiB` | Maximum number of bytes a node can take from the at-large allocation of the inbound message throttler. |

#### Message Based Rate-Limiting

Rate-limiting based on the number of unprocessed messages.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--throttler-inbound-node-max-processing-msgs` | `AVAGO_THROTTLER_INBOUND_NODE_MAX_PROCESSING_MSGS` | uint | `1024` | Node will stop reading messages from a peer when it is processing this many messages from the peer. Will resume reading messages from the peer when it is processing less than this many messages. |

#### Outbound Rate-Limiting

Rate-limiting for outbound messages.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--throttler-outbound-at-large-alloc-size` | `AVAGO_THROTTLER_OUTBOUND_AT_LARGE_ALLOC_SIZE` | uint | `32 MiB` | Size, in bytes, of at-large allocation in the outbound message throttler. |
| `--throttler-outbound-validator-alloc-size` | `AVAGO_THROTTLER_OUTBOUND_VALIDATOR_ALLOC_SIZE` | uint | `32 MiB` | Size, in bytes, of validator allocation in the outbound message throttler. |
| `--throttler-outbound-node-max-at-large-bytes` | `AVAGO_THROTTLER_OUTBOUND_NODE_MAX_AT_LARGE_BYTES` | uint | `2 MiB` | Maximum number of bytes a node can take from the at-large allocation of the outbound message throttler. |

### Connection Rate-Limiting

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--network-inbound-connection-throttling-cooldown` | `AVAGO_NETWORK_INBOUND_CONNECTION_THROTTLING_COOLDOWN` | duration | `10s` | Node will upgrade an inbound connection from a given IP at most once within this duration. If 0 or negative, will not consider recency of last upgrade when deciding whether to upgrade. |
| `--network-inbound-connection-throttling-max-conns-per-sec` | `AVAGO_NETWORK_INBOUND_CONNECTION_THROTTLING_MAX_CONNS_PER_SEC` | uint | `512` | Node will accept at most this many inbound connections per second. |
| `--network-outbound-connection-throttling-rps` | `AVAGO_NETWORK_OUTBOUND_CONNECTION_THROTTLING_RPS` | uint | `50` | Node makes at most this many outgoing peer connection attempts per second. |

### Peer List Gossiping

Nodes gossip peers to each other so that each node can have an up-to-date peer list. A node gossips `--network-peer-list-num-validator-ips` validator IPs to `--network-peer-list-validator-gossip-size` validators, `--network-peer-list-non-validator-gossip-size` non-validators and `--network-peer-list-peers-gossip-size` peers every `--network-peer-list-gossip-frequency`.

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--network-peer-list-num-validator-ips` | `AVAGO_NETWORK_PEER_LIST_NUM_VALIDATOR_IPS` | int | `15` | Number of validator IPs to gossip to other nodes. |
| `--network-peer-list-validator-gossip-size` | `AVAGO_NETWORK_PEER_LIST_VALIDATOR_GOSSIP_SIZE` | int | `20` | Number of validators that the node will gossip peer list to. |
| `--network-peer-list-non-validator-gossip-size` | `AVAGO_NETWORK_PEER_LIST_NON_VALIDATOR_GOSSIP_SIZE` | int | `0` | Number of non-validators that the node will gossip peer list to. |
| `--network-peer-list-peers-gossip-size` | `AVAGO_NETWORK_PEER_LIST_PEERS_GOSSIP_SIZE` | int | `0` | Number of total peers (including non-validator or validator) that the node will gossip peer list to. |
| `--network-peer-list-gossip-frequency` | `AVAGO_NETWORK_PEER_LIST_GOSSIP_FREQUENCY` | duration | `1m` | Frequency to gossip peers to other nodes. |
| `--network-peer-read-buffer-size` | `AVAGO_NETWORK_PEER_READ_BUFFER_SIZE` | int | `8 KiB` | Size of the buffer that peer messages are read into (there is one buffer per peer). |
| `--network-peer-write-buffer-size` | `AVAGO_NETWORK_PEER_WRITE_BUFFER_SIZE` | int | `8 KiB` | Size of the buffer that peer messages are written into (there is one buffer per peer). |

### Resource Usage Tracking

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--meter-vms-enabled` | `AVAGO_METER_VMS_ENABLED` | boolean | `true` | Enable Meter VMs to track VM performance with more granularity. |
| `--system-tracker-frequency` | `AVAGO_SYSTEM_TRACKER_FREQUENCY` | duration | `500ms` | Frequency to check the real system usage of tracked processes. More frequent checks -> usage metrics are more accurate, but more expensive to track. |
| `--system-tracker-processing-halflife` | `AVAGO_SYSTEM_TRACKER_PROCESSING_HALFLIFE` | duration | `15s` | Half life to use for the processing requests tracker. Larger half life -> usage metrics change more slowly. |
| `--system-tracker-cpu-halflife` | `AVAGO_SYSTEM_TRACKER_CPU_HALFLIFE` | duration | `15s` | Half life to use for the CPU tracker. Larger half life -> CPU usage metrics change more slowly. |
| `--system-tracker-disk-halflife` | `AVAGO_SYSTEM_TRACKER_DISK_HALFLIFE` | duration | `1m` | Half life to use for the disk tracker. Larger half life -> disk usage metrics change more slowly. |
| `--system-tracker-disk-required-available-space` | `AVAGO_SYSTEM_TRACKER_DISK_REQUIRED_AVAILABLE_SPACE` | uint | `536870912` | Minimum number of available bytes on disk, under which the node will shutdown. |
| `--system-tracker-disk-warning-threshold-available-space` | `AVAGO_SYSTEM_TRACKER_DISK_WARNING_THRESHOLD_AVAILABLE_SPACE` | uint | `1073741824` | Warning threshold for the number of available bytes on disk, under which the node will be considered unhealthy. Must be \>= `--system-tracker-disk-required-available-space`. |

### Plugins

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--plugin-dir` | `AVAGO_PLUGIN_DIR` | string | `$HOME/.avalanchego/plugins` | Sets the directory for [VM plugins](https://build.avax.network/docs/virtual-machines). |

### Virtual Machine (VM) Configs

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--vm-aliases-file` | `AVAGO_VM_ALIASES_FILE` | string | `~/.avalanchego/configs/vms/aliases.json` | Path to JSON file that defines aliases for Virtual Machine IDs. This flag is ignored if `--vm-aliases-file-content` is specified. Example content: `{"tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH": ["timestampvm", "timerpc"]}`. The above example aliases the VM whose ID is `"tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH"` to `"timestampvm"` and `"timerpc"`. |
| `--vm-aliases-file-content` | `AVAGO_VM_ALIASES_FILE_CONTENT` | string | - | As an alternative to `--vm-aliases-file`, it allows specifying base64 encoded aliases for Virtual Machine IDs. |

### Indexing

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--index-allow-incomplete` | `AVAGO_INDEX_ALLOW_INCOMPLETE` | boolean | `false` | If true, allow running the node in such a way that could cause an index to miss transactions. Ignored if index is disabled. |

### Router

| Flag | Env Var | Type | Default | Description |
|--------|--------|------|----|--------------------|
| `--router-health-max-drop-rate` | `AVAGO_ROUTER_HEALTH_MAX_DROP_RATE` | float | `1` | Node reports unhealthy if the router drops more than this portion of messages. |
| `--router-health-max-outstanding-requests` | `AVAGO_ROUTER_HEALTH_MAX_OUTSTANDING_REQUESTS` | uint | `1024` | Node reports unhealthy if there are more than this many outstanding consensus requests (Get, PullQuery, etc.) over all chains. |

## Additional Resources

- [Full documentation](https://build.avax.network/docs/quick-start)
- [Example configurations](https://github.com/ava-labs/avalanchego/tree/master/config)
- [Network upgrade schedules](https://build.avax.network/docs/quick-start/primary-network)

</div>
