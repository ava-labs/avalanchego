# AvalancheGo Configs and Flags

<!-- markdownlint-disable MD001 -->

You can specify the configuration of a node with the arguments below.

## APIs

#### `--api-admin-enabled` (boolean)

If set to `true`, this node will expose the Admin API. Defaults to `false`.
See [here](https://build.avax.network/docs/api-reference/admin-api) for more information.

#### `--api-health-enabled` (boolean)

If set to `false`, this node will not expose the Health API. Defaults to `true`. See
[here](https://build.avax.network/docs/api-reference/health-api) for more information.

#### `--index-enabled` (boolean)

If set to `true`, this node will enable the indexer and the Index API will be
available. Defaults to `false`. See
[here](https://build.avax.network/docs/api-reference/index-api) for more information.

#### `--api-info-enabled` (boolean)

If set to `false`, this node will not expose the Info API. Defaults to `true`. See
[here](https://build.avax.network/docs/api-reference/info-api) for more information.

#### `--api-metrics-enabled` (boolean)

If set to `false`, this node will not expose the Metrics API. Defaults to
`true`. See [here](https://build.avax.network/docs/api-reference/metrics-api) for more information.

## Avalanche Community Proposals

#### `--acp-support` (array of integers)

The `--acp-support` flag allows an AvalancheGo node to indicate support for a
set of [Avalanche Community Proposals](https://github.com/avalanche-foundation/ACPs).

#### `--acp-object` (array of integers)

The `--acp-object` flag allows an AvalancheGo node to indicate objection for a
set of [Avalanche Community Proposals](https://github.com/avalanche-foundation/ACPs).

## Bootstrapping

#### `--bootstrap-ancestors-max-containers-sent` (uint)

Max number of containers in an `Ancestors` message sent by this node. Defaults to `2000`.

#### `--bootstrap-ancestors-max-containers-received` (unit)

This node reads at most this many containers from an incoming `Ancestors` message. Defaults to `2000`.

#### `--bootstrap-beacon-connection-timeout` (duration)

Timeout when attempting to connect to bootstrapping beacons. Defaults to `1m`.

#### `--bootstrap-ids` (string)

Bootstrap IDs is a comma-separated list of validator IDs. These IDs will be used
to authenticate bootstrapping peers. An example setting of this field would be
`--bootstrap-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg,NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ"`.
The number of given IDs here must be same with number of given
`--bootstrap-ips`. The default value depends on the network ID.

#### `--bootstrap-ips` (string)

Bootstrap IPs is a comma-separated list of IP:port pairs. These IP Addresses
will be used to bootstrap the current Avalanche state. An example setting of
this field would be `--bootstrap-ips="127.0.0.1:12345,1.2.3.4:5678"`. The number
of given IPs here must be same with number of given `--bootstrap-ids`. The
default value depends on the network ID.

#### `--bootstrap-max-time-get-ancestors` (duration)

Max Time to spend fetching a container and its ancestors when responding to a GetAncestors message.
Defaults to `50ms`.

#### `--bootstrap-retry-enabled` (boolean)

If set to `false`, will not retry bootstrapping if it fails. Defaults to `true`.

#### `--bootstrap-retry-warn-frequency` (uint)

Specifies how many times bootstrap should be retried before warning the operator. Defaults to `50`.

## Chain Configs

Some blockchains allow the node operator to provide custom configurations for
individual blockchains. These custom configurations are broken down into two
categories: network upgrades and optional chain configurations. AvalancheGo
reads in these configurations from the chain configuration directory and passes
them into the VM on initialization.

#### `--chain-config-dir` (string)

Specifies the directory that contains chain configs, as described
[here](https://build.avax.network/docs/nodes/chain-configs). Defaults to `$HOME/.avalanchego/configs/chains`.
If this flag is not provided and the default directory does not exist,
AvalancheGo will not exit since custom configs are optional. However, if the
flag is set, the specified folder must exist, or AvalancheGo will exit with an
error. This flag is ignored if `--chain-config-content` is specified.

:::note
Please replace `chain-config-dir` and `blockchainID` with their actual values.
:::

Network upgrades are passed in from the location:
`chain-config-dir`/`blockchainID`/`upgrade.*`.
Upgrade files are typically json encoded and therefore named `upgrade.json`.
However, the format of the file is VM dependent.
After a blockchain has activated a network upgrade, the same upgrade
configuration must always be passed in to ensure that the network upgrades
activate at the correct time.

The chain configs are passed in from the location
`chain-config-dir`/`blockchainID`/`config.*`.
Upgrade files are typically json encoded and therefore named `upgrade.json`.
However, the format of the file is VM dependent.
This configuration is used by the VM to handle optional configuration flags such
as enabling/disabling APIs, updating log level, etc.
The chain configuration is intended to provide optional configuration parameters
and the VM will use default values if nothing is passed in.

Full reference for all configuration options for some standard chains can be
found in a separate [chain config flags](https://build.avax.network/docs/nodes/chain-configs) document.

Full reference for `subnet-evm` upgrade configuration can be found in a separate
[Customize a Subnet](https://build.avax.network/docs/avalanche-l1s/upgrade/customize-avalanche-l1) document.

#### `--chain-config-content` (string)

As an alternative to `--chain-config-dir`, chains custom configurations can be
loaded altogether from command line via `--chain-config-content` flag. Content
must be base64 encoded.

Example:

```bash
cchainconfig="$(echo -n '{"log-level":"trace"}' | base64)"
chainconfig="$(echo -n "{\"C\":{\"Config\":\"${cchainconfig}\",\"Upgrade\":null}}" | base64)"
avalanchego --chain-config-content "${chainconfig}"
```

#### `--chain-aliases-file` (string)

Path to JSON file that defines aliases for Blockchain IDs. Defaults to
`~/.avalanchego/configs/chains/aliases.json`. This flag is ignored if
`--chain-aliases-file-content` is specified. Example content:

```json
{
  "q2aTwKuyzgs8pynF7UXBZCU7DejbZbZ6EUyHr3JQzYgwNPUPi": ["DFK"]
}
```

The above example aliases the Blockchain whose ID is
`"q2aTwKuyzgs8pynF7UXBZCU7DejbZbZ6EUyHr3JQzYgwNPUPi"` to `"DFK"`. Chain
aliases are added after adding primary network aliases and before any changes to
the aliases via the admin API. This means that the first alias included for a
Blockchain on a Subnet will be treated as the `"Primary Alias"` instead of the
full blockchainID. The Primary Alias is used in all metrics and logs.

#### `--chain-aliases-file-content` (string)

As an alternative to `--chain-aliases-file`, it allows specifying base64 encoded
aliases for Blockchains.

#### `--chain-data-dir` (string)

Chain specific data directory. Defaults to `$HOME/.avalanchego/chainData`.

## Config File

#### `--config-file` (string)

Path to a JSON file that specifies this node's configuration. Command line
arguments will override arguments set in the config file. This flag is ignored
if `--config-file-content` is specified.

Example JSON config file:

```json
{
  "log-level": "debug"
}
```

:::tip
[Install Script](https://build.avax.network/docs/tooling/avalanche-go-installer) creates the
node config file at `~/.avalanchego/configs/node.json`. No default file is
created if [AvalancheGo is built from source](https://build.avax.network/docs/nodes/run-a-node/from-source), you
would need to create it manually if needed.
:::

#### `--config-file-content` (string)

As an alternative to `--config-file`, it allows specifying base64 encoded config
content.

#### `--config-file-content-type` (string)

Specifies the format of the base64 encoded config content. JSON, TOML, YAML are
among currently supported file format (see
[here](https://github.com/spf13/viper#reading-config-files) for full list). Defaults to `JSON`.

## Data Directory

#### `--data-dir` (string)

Sets the base data directory where default sub-directories will be placed unless otherwise specified.
Defaults to `$HOME/.avalanchego`.

## Database

##### `--db-dir` (string, file path)

Specifies the directory to which the database is persisted. Defaults to `"$HOME/.avalanchego/db"`.

##### `--db-type` (string)

Specifies the type of database to use. Must be one of `leveldb`, `memdb`, or `pebbledb`.
`memdb` is an in-memory, non-persisted database.

:::note

`memdb` stores everything in memory. So if you have a 900 GiB LevelDB instance, then using `memdb`
you’d need 900 GiB of RAM.
`memdb` is useful for fast one-off testing, not for running an actual node (on Fuji or Mainnet).
Also note that `memdb` doesn’t persist after restart. So any time you restart the node it would
start syncing from scratch.

:::

### Database Config

#### `--db-config-file` (string)

Path to the database config file. Ignored if `--config-file-content` is specified.

#### `--db-config-file-content` (string)

As an alternative to `--db-config-file`, it allows specifying base64 encoded database config content.

#### LevelDB Config

A LevelDB config file must be JSON and may have these keys.
Any keys not given will receive the default value.

```go
{
	// BlockCacheCapacity defines the capacity of the 'sorted table' block caching.
	// Use -1 for zero.
	//
	// The default value is 12MiB.
	"blockCacheCapacity": int,

	// BlockSize is the minimum uncompressed size in bytes of each 'sorted table'
	// block.
	//
	// The default value is 4KiB.
	"blockSize": int,

	// CompactionExpandLimitFactor limits compaction size after expanded.
	// This will be multiplied by table size limit at compaction target level.
	//
	// The default value is 25.
	"compactionExpandLimitFactor": int,

	// CompactionGPOverlapsFactor limits overlaps in grandparent (Level + 2)
	// that a single 'sorted table' generates.  This will be multiplied by
	// table size limit at grandparent level.
	//
	// The default value is 10.
	"compactionGPOverlapsFactor": int,

	// CompactionL0Trigger defines number of 'sorted table' at level-0 that will
	// trigger compaction.
	//
	// The default value is 4.
	"compactionL0Trigger": int,

	// CompactionSourceLimitFactor limits compaction source size. This doesn't apply to
	// level-0.
	// This will be multiplied by table size limit at compaction target level.
	//
	// The default value is 1.
	"compactionSourceLimitFactor": int,

	// CompactionTableSize limits size of 'sorted table' that compaction generates.
	// The limits for each level will be calculated as:
	//   CompactionTableSize * (CompactionTableSizeMultiplier ^ Level)
	// The multiplier for each level can also fine-tuned using CompactionTableSizeMultiplierPerLevel.
	//
	// The default value is 2MiB.
	"compactionTableSize": int,

	// CompactionTableSizeMultiplier defines multiplier for CompactionTableSize.
	//
	// The default value is 1.
	"compactionTableSizeMultiplier": float,

	// CompactionTableSizeMultiplierPerLevel defines per-level multiplier for
	// CompactionTableSize.
	// Use zero to skip a level.
	//
	// The default value is nil.
	"compactionTableSizeMultiplierPerLevel": []float,

	// CompactionTotalSize limits total size of 'sorted table' for each level.
	// The limits for each level will be calculated as:
	//   CompactionTotalSize * (CompactionTotalSizeMultiplier ^ Level)
	// The multiplier for each level can also fine-tuned using
	// CompactionTotalSizeMultiplierPerLevel.
	//
	// The default value is 10MiB.
	"compactionTotalSize": int,

	// CompactionTotalSizeMultiplier defines multiplier for CompactionTotalSize.
	//
	// The default value is 10.
	"compactionTotalSizeMultiplier": float,

	// DisableSeeksCompaction allows disabling 'seeks triggered compaction'.
	// The purpose of 'seeks triggered compaction' is to optimize database so
	// that 'level seeks' can be minimized, however this might generate many
	// small compaction which may not preferable.
	//
	// The default is true.
	"disableSeeksCompaction": bool,

	// OpenFilesCacheCapacity defines the capacity of the open files caching.
	// Use -1 for zero, this has same effect as specifying NoCacher to OpenFilesCacher.
	//
	// The default value is 1024.
	"openFilesCacheCapacity": int,

	// WriteBuffer defines maximum size of a 'memdb' before flushed to
	// 'sorted table'. 'memdb' is an in-memory DB backed by an on-disk
	// unsorted journal.
	//
	// LevelDB may held up to two 'memdb' at the same time.
	//
	// The default value is 6MiB.
	"writeBuffer": int,

	// FilterBitsPerKey is the number of bits to add to the bloom filter per
	// key.
	//
	// The default value is 10.
	"filterBitsPerKey": int,

	// MaxManifestFileSize is the maximum size limit of the MANIFEST-****** file.
	// When the MANIFEST-****** file grows beyond this size, LevelDB will create
	// a new MANIFEST file.
	//
	// The default value is infinity.
	"maxManifestFileSize": int,

	// MetricUpdateFrequency is the frequency to poll LevelDB metrics in
	// nanoseconds.
	// If <= 0, LevelDB metrics aren't polled.
	//
	// The default value is 10s.
	"metricUpdateFrequency": int,
}
```

## File Descriptor Limit

#### `--fd-limit` (int)

Attempts to raise the process file descriptor limit to at least this value and
error if the value is above the system max. Linux default `32768`.

## Genesis

#### `--genesis-file` (string)

Path to a JSON file containing the genesis data to use. Ignored when running
standard networks (Mainnet, Fuji Testnet), or when `--genesis-content` is
specified. If not given, uses default genesis data.

See the documentation for the genesis JSON format [here](../genesis/README.md) and an example used for the local network genesis [here](../genesis/genesis_local.json).

#### `--genesis-file-content` (string)

As an alternative to `--genesis-file`, it allows specifying base64 encoded genesis data to use.

## HTTP Server

#### `--http-allowed-hosts` (string)

List of acceptable host names in API requests. Provide the wildcard (`'*'`) to accept
requests from all hosts. API requests where the `Host` field is empty or an IP address
will always be accepted. An API call whose HTTP `Host` field isn't acceptable will
receive a 403 error code. Defaults to `localhost`.

#### `--http-allowed-origins` (string)

Origins to allow on the HTTP port. Defaults to `*` which allows all origins. Example:
`"https://*.avax.network https://*.avax-test.network"`

#### `--http-host` (string)

The address that HTTP APIs listen on. Defaults to `127.0.0.1`. This means that
by default, your node can only handle API calls made from the same machine. To
allow API calls from other machines, use `--http-host=`. You can also enter
domain names as parameter.

#### `--http-idle-timeout` (string)

Maximum duration to wait for the next request when keep-alives are enabled. If
`--http-idle-timeout` is zero, the value of `--http-read-timeout` is used. If both are zero,
there is no timeout.

#### `--http-port` (int)

Each node runs an HTTP server that provides the APIs for interacting with the
node and the Avalanche network. This argument specifies the port that the HTTP
server will listen on. The default value is `9650`.

#### `--http-read-timeout` (string)

Maximum duration for reading the entire request, including the body. A zero or
negative value means there will be no timeout.

#### `--http-read-header-timeout` (string)

Maximum duration to read request headers. The connection’s read deadline is
reset after reading the headers. If `--http-read-header-timeout` is zero, the
value of `--http-read-timeout` is used. If both are zero, there is no timeout.

#### `--http-shutdown-timeout` (duration)

Maximum duration to wait for existing connections to complete during node
shutdown. Defaults to `10s`.

#### `--http-shutdown-wait` (duration)

Duration to wait after receiving SIGTERM or SIGINT before initiating shutdown.
The `/health` endpoint will return unhealthy during this duration (if the Health
API is enabled.) Defaults to `0s`.

#### `--http-tls-cert-file` (string, file path)

This argument specifies the location of the TLS certificate used by the node for
the HTTPS server. This must be specified when `--http-tls-enabled=true`. There
is no default value. This flag is ignored if `--http-tls-cert-file-content` is
specified.

#### `--http-tls-cert-file-content` (string)

As an alternative to `--http-tls-cert-file`, it allows specifying base64 encoded
content of the TLS certificate used by the node for the HTTPS server. Note that
full certificate content, with the leading and trailing header, must be base64
encoded. This must be specified when `--http-tls-enabled=true`.

#### `--http-tls-enabled` (boolean)

If set to `true`, this flag will attempt to upgrade the server to use HTTPS. Defaults to `false`.

#### `--http-tls-key-file` (string, file path)

This argument specifies the location of the TLS private key used by the node for
the HTTPS server. This must be specified when `--http-tls-enabled=true`. There
is no default value. This flag is ignored if `--http-tls-key-file-content` is
specified.

#### `--http-tls-key-file-content` (string)

As an alternative to `--http-tls-key-file`, it allows specifying base64 encoded
content of the TLS private key used by the node for the HTTPS server. Note that
full private key content, with the leading and trailing header, must be base64
encoded. This must be specified when `--http-tls-enabled=true`.

#### `--http-write-timeout` (string)

Maximum duration before timing out writes of the response. It is reset whenever
a new request’s header is read. A zero or negative value means there will be no
timeout.

## Logging

#### `--log-level` (string, `{verbo, debug, trace, info, warn, error, fatal, off}`)

The log level determines which events to log. There are 8 different levels, in
order from highest priority to lowest.

- `off`: No logs have this level of logging. Turns off logging.
- `fatal`: Fatal errors that are not recoverable.
- `error`: Errors that the node encounters, these errors were able to be recovered.
- `warn`: A Warning that might be indicative of a spurious byzantine node, or potential future error.
- `info`: Useful descriptions of node status updates.
- `trace`: Traces container (block, vertex, transaction) job results. Useful for
  tracing container IDs and their outcomes.
- `debug`: Debug logging is useful when attempting to understand possible bugs
  in the code. More information that would be typically desired for normal usage
  will be displayed.
- `verbo`: Tracks extensive amounts of information the node is processing. This
  includes message contents and binary dumps of data for extremely low level
  protocol analysis.

When specifying a log level note that all logs with the specified priority or
higher will be tracked. Defaults to `info`.

#### `--log-display-level` (string, `{verbo, debug, trace, info, warn, error, fatal, off}`)

The log level determines which events to display to stdout. If left blank,
will default to the value provided to `--log-level`.

#### `--log-format` (string, `{auto, plain, colors, json}`)

The structure of log format. Defaults to `auto` which formats terminal-like
logs, when the output is a terminal. Otherwise, should be one of `{auto, plain, colors, json}`

#### `--log-dir` (string, file path)

Specifies the directory in which system logs are kept. Defaults to `"$HOME/.avalanchego/logs"`.
If you are running the node as a system service (ex. using the installer script) logs will also be

#### `--log-disable-display-plugin-logs` (boolean)

Disables displaying plugin logs in stdout. Defaults to `false`.

#### `--log-rotater-max-size` (uint)

The maximum file size in megabytes of the log file before it gets rotated. Defaults to `8`.

#### `--log-rotater-max-files` (uint)

The maximum number of old log files to retain. 0 means retain all old log files. Defaults to `7`.

#### `--log-rotater-max-age` (uint)

The maximum number of days to retain old log files based on the timestamp
encoded in their filename. 0 means retain all old log files. Defaults to `0`.

#### `--log-rotater-compress-enabled` (boolean)

Enables the compression of rotated log files through gzip. Defaults to `false`.

## Network ID

#### `--network-id` (string)

The identity of the network the node should connect to. Can be one of:

- `--network-id=mainnet` -&gt; Connect to Mainnet (default).
- `--network-id=fuji` -&gt; Connect to the Fuji test-network.
- `--network-id=testnet` -&gt; Connect to the current test-network. (Right now, this is Fuji.)
- `--network-id=local` -&gt; Connect to a local test-network.
- `--network-id=network-{id}` -&gt; Connect to the network with the given ID.
  `id` must be in the range `[0, 2^32)`.

## OpenTelemetry

AvalancheGo supports collecting and exporting [OpenTelemetry](https://opentelemetry.io/) traces.
This might be useful for debugging, performance analysis, or monitoring.

#### `--tracing-endpoint` (string)

The endpoint to export trace data to. Defaults to `localhost:4317` if `--tracing-exporter-type` is set to `grpc` and `localhost:4318` if `--tracing-exporter-type` is set to `http`.

#### `--tracing-exporter-type`(string)

Type of exporter to use for tracing. Options are [`disabled`,`grpc`,`http`]. Defaults to `disabled`.

#### `--tracing-insecure` (string)

If true, don't use TLS when exporting trace data. Defaults to `true`.

#### `--tracing-sample-rate` (float)

The fraction of traces to sample. If >= 1, always sample. If `<= 0`, never sample.
Defaults to `0.1`.

## Partial Sync Primary Network

#### `--partial-sync-primary-network` (string)

Partial sync enables nodes that are not primary network validators to optionally sync
only the P-chain on the primary network. Nodes that use this option can still track
Subnets. After the Etna upgrade, nodes that use this option can also validate L1s.
This config defaults to `false`.

## Public IP

Validators must know one of their public facing IP addresses so they can enable
other nodes to connect to them.

By default, the node will attempt to perform NAT traversal to get the node's IP
according to its router.

#### `--public-ip` (string)

If this argument is provided, the node assumes this is its public IP.

:::tip
When running a local network it may be easiest to set this value to `127.0.0.1`.
:::

#### `--public-ip-resolution-frequency` (duration)

Frequency at which this node resolves/updates its public IP and renew NAT
mappings, if applicable. Default to 5 minutes.

#### `--public-ip-resolution-service` (string)

When provided, the node will use that service to periodically resolve/update its
public IP. Only acceptable values are `ifconfigCo`, `opendns` or `ifconfigMe`.

## State Syncing

#### `--state-sync-ids` (string)

State sync IDs is a comma-separated list of validator IDs. The specified
validators will be contacted to get and authenticate the starting point (state
summary) for state sync. An example setting of this field would be
`--state-sync-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg,NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ"`.
The number of given IDs here must be same with number of given
`--state-sync-ips`. The default value is empty, which results in all validators
being sampled.

#### `--state-sync-ips` (string)

State sync IPs is a comma-separated list of IP:port pairs. These IP Addresses
will be contacted to get and authenticate the starting point (state summary) for
state sync. An example setting of this field would be
`--state-sync-ips="127.0.0.1:12345,1.2.3.4:5678"`. The number of given IPs here
must be the same with the number of given `--state-sync-ids`.

## Staking

#### `--staking-port` (int)

The port through which the network peers will connect to this node externally.
Having this port accessible from the internet is required for correct node
operation. Defaults to `9651`.

#### `--sybil-protection-enabled` (boolean)

Avalanche uses Proof of Stake (PoS) as sybil resistance to make it prohibitively
expensive to attack the network. If false, sybil resistance is disabled and all
peers will be sampled during consensus. Defaults to `true`. Note that this can
not be disabled on public networks (`Fuji` and `Mainnet`).

Setting this flag to `false` **does not** mean "this node is not a validator."
It means that this node will sample all nodes, not just validators.
**You should not set this flag to false unless you understand what you are doing.**

#### `--sybil-protection-disabled-weight` (uint)

Weight to provide to each peer when staking is disabled. Defaults to `100`.

#### `--staking-tls-cert-file` (string, file path)

Avalanche uses two-way authenticated TLS connections to securely connect nodes.
This argument specifies the location of the TLS certificate used by the node. By
default, the node expects the TLS certificate to be at
`$HOME/.avalanchego/staking/staker.crt`. This flag is ignored if
`--staking-tls-cert-file-content` is specified.

#### `--staking-tls-cert-file-content` (string)

As an alternative to `--staking-tls-cert-file`, it allows specifying base64
encoded content of the TLS certificate used by the node. Note that full
certificate content, with the leading and trailing header, must be base64
encoded.

#### `--staking-tls-key-file` (string, file path)

Avalanche uses two-way authenticated TLS connections to securely connect nodes.
This argument specifies the location of the TLS private key used by the node. By
default, the node expects the TLS private key to be at
`$HOME/.avalanchego/staking/staker.key`. This flag is ignored if
`--staking-tls-key-file-content` is specified.

#### `--staking-tls-key-file-content` (string)

As an alternative to `--staking-tls-key-file`, it allows specifying base64
encoded content of the TLS private key used by the node. Note that full private
key content, with the leading and trailing header, must be base64 encoded.

## Subnets

### Subnet Tracking

#### `--track-subnets` (string)

Comma separated list of Subnet IDs that this node would track if added to.
Defaults to empty (will only validate the Primary Network).

### Subnet Configs

It is possible to provide parameters for Subnets. Parameters here apply to all
chains in the specified Subnets. Parameters must be specified with a
`{subnetID}.json` config file under `--subnet-config-dir`. AvalancheGo loads
configs for Subnets specified in
`--track-subnets` parameter.

Full reference for all configuration options for a Subnet can be found in a
separate [Subnet Configs](https://build.avax.network/docs/nodes/configure/avalanche-l1-configs) document.

#### `--subnet-config-dir` (`string`)

Specifies the directory that contains Subnet configs, as described above.
Defaults to `$HOME/.avalanchego/configs/subnets`. If the flag is set explicitly,
the specified folder must exist, or AvalancheGo will exit with an error. This
flag is ignored if `--subnet-config-content` is specified.

Example: Let's say we have a Subnet with ID
`p4jUwqZsA2LuSftroCd3zb4ytH8W99oXKuKVZdsty7eQ3rXD6`. We can create a config file
under the default `subnet-config-dir` at
`$HOME/.avalanchego/configs/subnets/p4jUwqZsA2LuSftroCd3zb4ytH8W99oXKuKVZdsty7eQ3rXD6.json`.
An example config file is:

```json
{
  "validatorOnly": false,
  "consensusParameters": {
    "k": 25,
    "alpha": 18
  }
}
```

:::tip
By default, none of these directories and/or files exist. You would need to create them manually if needed.
:::

#### `--subnet-config-content` (string)

As an alternative to `--subnet-config-dir`, it allows specifying base64 encoded parameters for a Subnet.

## Version

#### `--version` (boolean)

If this is `true`, print the version and quit. Defaults to `false`.

## Advanced Options

The following options may affect the correctness of a node. Only power users should change these.

### Gossiping

#### `--consensus-accepted-frontier-gossip-validator-size` (uint)

Number of validators to gossip to when gossiping accepted frontier. Defaults to `0`.

#### `--consensus-accepted-frontier-gossip-non-validator-size` (uint)

Number of non-validators to gossip to when gossiping accepted frontier. Defaults to `0`.

#### `--consensus-accepted-frontier-gossip-peer-size` (uint)

Number of peers to gossip to when gossiping accepted frontier. Defaults to `15`.

#### `--consensus-accepted-frontier-gossip-frequency` (duration)

Time between gossiping accepted frontiers. Defaults to `10s`.

#### `--consensus-on-accept-gossip-validator-size` (uint)

Number of validators to gossip to each accepted container to. Defaults to `0`.

#### `--consensus-on-accept-gossip-non-validator-size` (uint)

Number of non-validators to gossip to each accepted container to. Defaults to `0`.

#### `--consensus-on-accept-gossip-peer-size` (uint)

Number of peers to gossip to each accepted container to. Defaults to `10`.

### Benchlist

#### `--benchlist-duration` (duration)

Maximum amount of time a peer is benchlisted after surpassing
`--benchlist-fail-threshold`. Defaults to `15m`.

#### `--benchlist-fail-threshold` (int)

Number of consecutive failed queries to a node before benching it (assuming all
queries to it will fail). Defaults to `10`.

#### `--benchlist-min-failing-duration` (duration)

Minimum amount of time queries to a peer must be failing before the peer is benched. Defaults to `150s`.

### Consensus Parameters

:::note
Some of these parameters can only be set on a local or private network, not on Fuji Testnet or Mainnet
:::

#### `--consensus-shutdown-timeout` (duration)

Timeout before killing an unresponsive chain. Defaults to `5s`.

#### `--create-asset-tx-fee` (int)

Transaction fee, in nAVAX, for transactions that create new assets. Defaults to
`10000000` nAVAX (.01 AVAX) per transaction. This can only be changed on a local
network.

#### `--min-delegator-stake` (int)

The minimum stake, in nAVAX, that can be delegated to a validator of the Primary Network.

Defaults to `25000000000` (25 AVAX) on Mainnet. Defaults to `5000000` (.005
AVAX) on Test Net. This can only be changed on a local network.

#### `--min-delegation-fee` (int)

The minimum delegation fee that can be charged for delegation on the Primary
Network, multiplied by `10,000` . Must be in the range `[0, 1000000]`. Defaults
to `20000` (2%) on Mainnet. This can only be changed on a local network.

#### `--min-stake-duration` (duration)

Minimum staking duration. The Default on Mainnet is `336h` (two weeks). This can only be changed on
a local network. This applies to both delegation and validation periods.

#### `--min-validator-stake` (int)

The minimum stake, in nAVAX, required to validate the Primary Network. This can
only be changed on a local network.

Defaults to `2000000000000` (2,000 AVAX) on Mainnet. Defaults to `5000000` (.005 AVAX) on Test Net.

#### `--max-stake-duration` (duration)

The maximum staking duration, in hours. Defaults to `8760h` (365 days) on
Mainnet. This can only be changed on a local network.

#### `--max-validator-stake` (int)

The maximum stake, in nAVAX, that can be placed on a validator on the primary
network. Defaults to `3000000000000000` (3,000,000 AVAX) on Mainnet. This
includes stake provided by both the validator and by delegators to the
validator. This can only be changed on a local network.

#### `--stake-minting-period` (duration)

Consumption period of the staking function, in hours. The Default on Mainnet is
`8760h` (365 days). This can only be changed on a local network.

#### `--stake-max-consumption-rate` (uint)

The maximum percentage of the consumption rate for the remaining token supply in
the minting period, which is 1 year on Mainnet. Defaults to `120,000` which is
12% per years. This can only be changed on a local network.

#### `--stake-min-consumption-rate` (uint)

The minimum percentage of the consumption rate for the remaining token supply in
the minting period, which is 1 year on Mainnet. Defaults to `100,000` which is
10% per years. This can only be changed on a local network.

#### `--stake-supply-cap` (uint)

The maximum stake supply, in nAVAX, that can be placed on a validator. Defaults
to `720,000,000,000,000,000` nAVAX. This can only be changed on a local network.

#### `--tx-fee` (int)

The required amount of nAVAX to be burned for a transaction to be valid on the
X-Chain, and for import/export transactions on the P-Chain. This parameter
requires network agreement in its current form. Changing this value from the
default should only be done on private networks or local network. Defaults to
`1,000,000` nAVAX per transaction.

#### `--uptime-requirement` (float)

Fraction of time a validator must be online to receive rewards. Defaults to
`0.8`. This can only be changed on a local network.

#### `--uptime-metric-freq` (duration)

Frequency of renewing this node's average uptime metric. Defaults to `30s`.

#### Simplex Parameters

##### `simplex-enabled` (bool)

Substitutes Snow’s consensus engine with Simplex’s consensus engine. Defaults to `false`.

#### Snow Parameters

##### `--snow-concurrent-repolls` (int)

Snow consensus requires repolling transactions that are issued during low time
of network usage. This parameter lets one define how aggressive the client will
be in finalizing these pending transactions. This should only be changed after
careful consideration of the tradeoffs of Snow consensus. The value must be at
least `1` and at most `--snow-commit-threshold`. Defaults to `4`.

##### `--snow-sample-size` (int)

Snow consensus defines `k` as the number of validators that are sampled during
each network poll. This parameter lets one define the `k` value used for
consensus. This should only be changed after careful consideration of the
tradeoffs of Snow consensus. The value must be at least `1`. Defaults to `20`.

##### `--snow-quorum-size` (int)

Snow consensus defines `alpha` as the number of validators that must prefer a
transaction during each network poll to increase the confidence in the
transaction. This parameter lets us define the `alpha` value used for consensus.
This should only be changed after careful consideration of the tradeoffs of Snow
consensus. The value must be at greater than `k/2`. Defaults to `15`.

##### `--snow-commit-threshold` (int)

Snow consensus defines `beta` as the number of consecutive polls that a
container must increase its confidence for it to be accepted. This
parameter lets us define the `beta` value used for consensus. This should only
be changed after careful consideration of the tradeoffs of Snow consensus. The
value must be at least `1`. Defaults to `20`.

##### `--snow-optimal-processing` (int)

Optimal number of processing items in consensus. The value must be at least `1`. Defaults to `50`.

##### `--snow-max-processing` (int)

Maximum number of processing items to be considered healthy. Reports unhealthy
if more than this number of items are outstanding. The value must be at least
`1`. Defaults to `1024`.

##### `--snow-max-time-processing` (duration)

Maximum amount of time an item should be processing and still be healthy.
Reports unhealthy if there is an item processing for longer than this duration.
The value must be greater than `0`. Defaults to `2m`.

### ProposerVM Parameters

#### `--proposervm-use-current-height` (bool)

Have the ProposerVM always report the last accepted P-chain block height. Defaults to `false`.

### `--proposervm-min-block-delay` (duration)

The minimum delay to enforce when building a snowman++ block for the primary network
chains and the default minimum delay for subnets. Defaults to `1s`. A non-default
value is only suggested for non-production nodes.

### Continuous Profiling

You can configure your node to continuously run memory/CPU profiles and save the
most recent ones. Continuous memory/CPU profiling is enabled if
`--profile-continuous-enabled` is set.

#### `--profile-continuous-enabled` (boolean)

Whether the app should continuously produce performance profiles. Defaults to the false (not enabled).

#### `--profile-dir` (string)

If profiling enabled, node continuously runs memory/CPU profiles and puts them
at this directory. Defaults to the `$HOME/.avalanchego/profiles/`.

#### `--profile-continuous-freq` (duration)

How often a new CPU/memory profile is created. Defaults to `15m`.

#### `--profile-continuous-max-files` (int)

Maximum number of CPU/memory profiles files to keep. Defaults to 5.

### Health

#### `--health-check-frequency` (duration)

Health check runs with this frequency. Defaults to `30s`.

#### `--health-check-averager-halflife` (duration)

Half life of averagers used in health checks (to measure the rate of message
failures, for example.) Larger value --&gt; less volatile calculation of
averages. Defaults to `10s`.

### Network

#### `--network-allow-private-ips` (bool)

Allows the node to connect peers with private IPs. Defaults to `true`.

#### `--network-compression-type` (string)

The type of compression to use when sending messages to peers. Defaults to `gzip`.
Must be one of [`gzip`, `zstd`, `none`].

Nodes can handle inbound `gzip` compressed messages but by default send `zstd` compressed messages.

#### `--network-initial-timeout` (duration)

Initial timeout value of the adaptive timeout manager. Defaults to `5s`.

#### `--network-initial-reconnect-delay` (duration)

Initial delay duration must be waited before attempting to reconnect a peer. Defaults to `1s`.

#### `--network-max-reconnect-delay` (duration)

Maximum delay duration must be waited before attempting to reconnect a peer. Defaults to `1h`.

#### `--network-minimum-timeout` (duration)

Minimum timeout value of the adaptive timeout manager. Defaults to `2s`.

#### `--network-maximum-timeout` (duration)

Maximum timeout value of the adaptive timeout manager. Defaults to `10s`.

#### `--network-maximum-inbound-timeout` (duration)

Maximum timeout value of an inbound message. Defines duration within which an
incoming message must be fulfilled. Incoming messages containing deadline higher
than this value will be overridden with this value. Defaults to `10s`.

#### `--network-timeout-halflife` (duration)

Half life used when calculating average network latency. Larger value --&gt; less
volatile network latency calculation. Defaults to `5m`.

#### `--network-timeout-coefficient` (duration)

Requests to peers will time out after \[`network-timeout-coefficient`\] \*
\[average request latency\]. Defaults to `2`.

#### `--network-read-handshake-timeout` (duration)

Timeout value for reading handshake messages. Defaults to `15s`.

#### `--network-ping-timeout` (duration)

Timeout value for Ping-Pong with a peer. Defaults to `30s`.

#### `--network-ping-frequency` (duration)

Frequency of pinging other peers. Defaults to `22.5s`.

#### `--network-health-min-conn-peers` (uint)

Node will report unhealthy if connected to less than this many peers. Defaults to `1`.

#### `--network-health-max-time-since-msg-received` (duration)

Node will report unhealthy if it hasn't received a message for this amount of time. Defaults to `1m`.

#### `--network-health-max-time-since-msg-sent` (duration)

Network layer returns unhealthy if haven't sent a message for at least this much time. Defaults to `1m`.

#### `--network-health-max-portion-send-queue-full` (float)

Node will report unhealthy if its send queue is more than this portion full.
Must be in \[0,1\]. Defaults to `0.9`.

#### `--network-health-max-send-fail-rate` (float)

Node will report unhealthy if more than this portion of message sends fail. Must
be in \[0,1\]. Defaults to `0.25`.

#### `--network-health-max-outstanding-request-duration` (duration)

Node reports unhealthy if there has been a request outstanding for this duration. Defaults to `5m`.

#### `--network-max-clock-difference` (duration)

Max allowed clock difference value between this node and peers. Defaults to `1m`.

#### `--network-require-validator-to-connect` (bool)

If true, this node will only maintain a connection with another node if this
node is a validator, the other node is a validator, or the other node is a
beacon.

#### `--network-tcp-proxy-enabled` (bool)

Require all P2P connections to be initiated with a TCP proxy header. Defaults to `false`.

#### `--network-tcp-proxy-read-timeout` (duration)

Maximum duration to wait for a TCP proxy header. Defaults to `3s`.

#### `--network-outbound-connection-timeout` (duration)

Timeout while dialing a peer. Defaults to `30s`.

### Message Rate-Limiting

These flags govern rate-limiting of inbound and outbound messages. For more
information on rate-limiting and the flags below, see package `throttling` in
AvalancheGo.

#### CPU Based

Rate-limiting based on how much CPU usage a peer causes.

##### `--throttler-inbound-cpu-validator-alloc` (float)

Number of CPU allocated for use by validators. Value should be in range (0, total core count].
Defaults to half of the number of CPUs on the machine.

##### `--throttler-inbound-cpu-max-recheck-delay` (duration)

In the CPU rate-limiter, check at least this often whether the node's CPU usage
has fallen to an acceptable level. Defaults to `5s`.

##### `--throttler-inbound-disk-max-recheck-delay` (duration)

In the disk-based network throttler, check at least this often whether the node's disk usage has
fallen to an acceptable level. Defaults to `5s`.

##### `--throttler-inbound-cpu-max-non-validator-usage` (float)

Number of CPUs that if fully utilized, will rate limit all non-validators. Value should be in range
[0, total core count].
Defaults to %80 of the number of CPUs on the machine.

##### `--throttler-inbound-cpu-max-non-validator-node-usage` (float)

Maximum number of CPUs that a non-validator can utilize. Value should be in range [0, total core count].
Defaults to the number of CPUs / 8.

##### `--throttler-inbound-disk-validator-alloc` (float)

Maximum number of disk reads/writes per second to allocate for use by validators. Must be > 0.
Defaults to `1000 GiB/s`.

##### `--throttler-inbound-disk-max-non-validator-usage` (float)

Number of disk reads/writes per second that, if fully utilized, will rate limit all non-validators.
Must be >= 0.
Defaults to `1000 GiB/s`.

##### `--throttler-inbound-disk-max-non-validator-node-usage` (float)

Maximum number of disk reads/writes per second that a non-validator can utilize. Must be >= 0.
Defaults to `1000 GiB/s`.

#### Bandwidth Based

Rate-limiting based on the bandwidth a peer uses.

##### `--throttler-inbound-bandwidth-refill-rate` (uint)

Max average inbound bandwidth usage of a peer, in bytes per second. See
interface `throttling.BandwidthThrottler`. Defaults to `512`.

##### `--throttler-inbound-bandwidth-max-burst-size` (uint)

Max inbound bandwidth a node can use at once. See interface
`throttling.BandwidthThrottler`. Defaults to `2 MiB`.

#### Message Size Based

Rate-limiting based on the total size, in bytes, of unprocessed messages.

##### `--throttler-inbound-at-large-alloc-size` (uint)

Size, in bytes, of at-large allocation in the inbound message throttler. Defaults to `6291456` (6 MiB).

##### `--throttler-inbound-validator-alloc-size` (uint)

Size, in bytes, of validator allocation in the inbound message throttler.
Defaults to `33554432` (32 MiB).

##### `--throttler-inbound-node-max-at-large-bytes` (uint)

Maximum number of bytes a node can take from the at-large allocation of the
inbound message throttler. Defaults to `2097152` (2 MiB).

#### Message Based

Rate-limiting based on the number of unprocessed messages.

##### `--throttler-inbound-node-max-processing-msgs` (uint)

Node will stop reading messages from a peer when it is processing this many messages from the peer.
Will resume reading messages from the peer when it is processing less than this many messages.
Defaults to `1024`.

#### Outbound

Rate-limiting for outbound messages.

##### `--throttler-outbound-at-large-alloc-size` (uint)

Size, in bytes, of at-large allocation in the outbound message throttler.
Defaults to `33554432` (32 MiB).

##### `--throttler-outbound-validator-alloc-size` (uint)

Size, in bytes, of validator allocation in the outbound message throttler.
Defaults to `33554432` (32 MiB).

##### `--throttler-outbound-node-max-at-large-bytes` (uint)

Maximum number of bytes a node can take from the at-large allocation of the
outbound message throttler. Defaults to `2097152` (2 MiB).

### Connection Rate-Limiting

#### `--network-inbound-connection-throttling-cooldown` (duration)

Node will upgrade an inbound connection from a given IP at most once within this
duration. Defaults to `10s`. If 0 or negative, will not consider recency of last
upgrade when deciding whether to upgrade.

#### `--network-inbound-connection-throttling-max-conns-per-sec` (uint)

Node will accept at most this many inbound connections per second. Defaults to `512`.

#### `--network-outbound-connection-throttling-rps` (uint)

Node makes at most this many outgoing peer connection attempts per second. Defaults to `50`.

### Peer List Gossiping

Nodes gossip peers to each other so that each node can have an up-to-date peer
list. A node gossips `--network-peer-list-num-validator-ips` validator IPs to
`--network-peer-list-validator-gossip-size` validators,
`--network-peer-list-non-validator-gossip-size` non-validators and
`--network-peer-list-peers-gossip-size` peers every
`--network-peer-list-gossip-frequency`.

#### `--network-peer-list-num-validator-ips` (int)

Number of validator IPs to gossip to other nodes Defaults to `15`.

#### `--network-peer-list-validator-gossip-size` (int)

Number of validators that the node will gossip peer list to. Defaults to `20`.

#### `--network-peer-list-non-validator-gossip-size` (int)

Number of non-validators that the node will gossip peer list to. Defaults to `0`.

#### `--network-peer-list-peers-gossip-size` (int)

Number of total peers (including non-validator or validator) that the node will gossip peer list to
Defaults to `0`.

#### `--network-peer-list-gossip-frequency` (duration)

Frequency to gossip peers to other nodes. Defaults to `1m`.

#### `--network-peer-read-buffer-size` (int)

Size of the buffer that peer messages are read into (there is one buffer per
peer), defaults to `8` KiB (8192 Bytes).

#### `--network-peer-write-buffer-size` (int)

Size of the buffer that peer messages are written into (there is one buffer per
peer), defaults to `8` KiB (8192 Bytes).

### Resource Usage Tracking

#### `--meter-vm-enabled` (bool)

Enable Meter VMs to track VM performance with more granularity. Defaults to `true`.

#### `--system-tracker-frequency` (duration)

Frequency to check the real system usage of tracked processes. More frequent
checks --> usage metrics are more accurate, but more expensive to track.
Defaults to `500ms`.

#### `--system-tracker-processing-halflife` (duration)

Half life to use for the processing requests tracker. Larger half life --> usage
metrics change more slowly. Defaults to `15s`.

#### `--system-tracker-cpu-halflife` (duration)

Half life to use for the CPU tracker. Larger half life --> CPU usage metrics
change more slowly. Defaults to `15s`.

#### `--system-tracker-disk-halflife` (duration)

Half life to use for the disk tracker. Larger half life --> disk usage metrics
change more slowly. Defaults to `1m`.

#### `--system-tracker-disk-required-available-space` (uint)

"Minimum number of available bytes on disk, under which the node will shutdown.
Defaults to `536870912` (512 MiB).

#### `--system-tracker-disk-warning-threshold-available-space` (uint)

Warning threshold for the number of available bytes on disk, under which the
node will be considered unhealthy. Must be >=
`--system-tracker-disk-required-available-space`. Defaults to `1073741824` (1
GiB).

### Plugins

#### `--plugin-dir` (string)

Sets the directory for [VM plugins](https://build.avax.network/docs/virtual-machines). The default value is `$HOME/.avalanchego/plugins`.

### Virtual Machine (VM) Configs

#### `--vm-aliases-file (string)`

Path to JSON file that defines aliases for Virtual Machine IDs. Defaults to
`~/.avalanchego/configs/vms/aliases.json`. This flag is ignored if
`--vm-aliases-file-content` is specified. Example content:

```json
{
  "tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH": [
    "timestampvm",
    "timerpc"
  ]
}
```

The above example aliases the VM whose ID is
`"tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH"` to `"timestampvm"` and
`"timerpc"`.

`--vm-aliases-file-content` (string)

As an alternative to `--vm-aliases-file`, it allows specifying base64 encoded
aliases for Virtual Machine IDs.

### Indexing

#### `--index-allow-incomplete` (boolean)

If true, allow running the node in such a way that could cause an index to miss transactions.
Ignored if index is disabled. Defaults to `false`.

### Router

#### `--router-health-max-drop-rate` (float)

Node reports unhealthy if the router drops more than this portion of messages. Defaults to `1`.

#### `--router-health-max-outstanding-requests` (uint)

Node reports unhealthy if there are more than this many outstanding consensus requests
(Get, PullQuery, etc.) over all chains. Defaults to `1024`.
