# AvalancheGo Configuration

This document lists all available configuration options for AvalancheGo nodes. You can configure your node using either command-line flags or environment variables.

## Environment Variable Naming Convention

All environment variables follow the pattern: `AVAGO_{FLAG_NAME}` where the flag name is converted to uppercase with hyphens replaced by underscores.

For example:
- Flag: `--api-admin-enabled`
- Environment Variable: `AVAGO_API_ADMIN_ENABLED`

**Note:** In the tables below, environment variables are shown without the `AVAGO_` prefix for brevity. All environment variables must be prefixed with `AVAGO_`.

## Configuration Options

### APIs

Configuration for various APIs exposed by the node.

| Flag | Env Var | Type | Default | Description |
|------|---------|------|---------|-------------|
| `--api-admin-enabled` | `API_ADMIN_ENABLED` | bool | `false` | Enables the Admin API. Provides administrative functions for node management including adding/removing validators, managing aliases, and other privileged operations. See [Admin API documentation](https://build.avax.network/docs/api-reference/admin-api) |
| `--api-health-enabled` | `API_HEALTH_ENABLED` | bool | `true` | Enables the Health API. Allows monitoring of node health status, readiness checks, and liveness probes. Essential for containerized deployments and monitoring systems. See [Health API documentation](https://build.avax.network/docs/api-reference/health-api) |
| `--index-enabled` | `INDEX_ENABLED` | bool | `false` | Enables the indexer and Index API. Required for querying historical blockchain data, searching transactions, and retrieving detailed blockchain analytics. Note: Enabling indexing increases disk usage. See [Index API documentation](https://build.avax.network/docs/api-reference/index-api) |
| `--api-info-enabled` | `API_INFO_ENABLED` | bool | `true` | Enables the Info API. Provides general information about the node including version, network ID, peer information, and blockchain details. Commonly used for node monitoring and debugging. See [Info API documentation](https://build.avax.network/docs/api-reference/info-api) |
| `--api-metrics-enabled` | `API_METRICS_ENABLED` | bool | `true` | Enables the Metrics API. Exposes Prometheus-compatible metrics for monitoring node performance, resource usage, and blockchain statistics. Essential for observability setups. See [Metrics API documentation](https://build.avax.network/docs/api-reference/metrics-api) |

### Avalanche Community Proposals

Support for [Avalanche Community Proposals](https://github.com/avalanche-foundation/ACPs).

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--acp-support` | `AVAGO_ACP_SUPPORT` | []int | `[]` | List of ACP IDs to support |
| `--acp-object` | `AVAGO_ACP_OBJECT` | []int | `[]` | List of ACP IDs to object to |

### Bootstrapping

Configuration for node bootstrapping process.

| Flag | Env Var | Type | Default | Description |
|------|---------|------|---------|-------------|
| `--bootstrap-ancestors-max-containers-sent` | `BOOTSTRAP_ANCESTORS_MAX_CONTAINERS_SENT` | uint | `2000` | Maximum number of containers to include in an Ancestors message. Higher values can speed up bootstrapping but increase bandwidth usage. Consider network conditions when adjusting. |
| `--bootstrap-ancestors-max-containers-received` | `BOOTSTRAP_ANCESTORS_MAX_CONTAINERS_RECEIVED` | uint | `2000` | Maximum number of containers to process from incoming Ancestors messages. Protects against excessive memory usage from malicious or misconfigured peers. Should match or exceed the sent limit. |
| `--bootstrap-beacon-connection-timeout` | `BOOTSTRAP_BEACON_CONNECTION_TIMEOUT` | dur | `1m` | Maximum time to wait when connecting to bootstrap beacon nodes. Increase in high-latency environments or decrease for faster failure detection. |
| `--bootstrap-ids` | `BOOTSTRAP_IDS` | string | network | Comma-separated list of validator node IDs to use for bootstrapping. Must specify trusted validators. Example: `NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg,NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ`. Count must match `--bootstrap-ips`. |
| `--bootstrap-ips` | `BOOTSTRAP_IPS` | string | network | Comma-separated list of IP:port pairs corresponding to bootstrap node IDs. Example: `127.0.0.1:9651,1.2.3.4:9651`. Count must match `--bootstrap-ids`. Default port is 9651 if not specified. |
| `--bootstrap-max-time-get-ancestors` | `BOOTSTRAP_MAX_TIME_GET_ANCESTORS` | dur | `50ms` | Maximum time to spend fetching a container and its ancestors when responding to GetAncestors requests. Prevents long-running requests from blocking other operations. |
| `--bootstrap-retry-enabled` | `BOOTSTRAP_RETRY_ENABLED` | bool | `true` | Whether to retry bootstrapping if initial attempt fails. Disabling prevents automatic recovery from temporary network issues during startup. |
| `--bootstrap-retry-warn-frequency` | `BOOTSTRAP_RETRY_WARN_FREQUENCY` | uint | `50` | Number of bootstrap retry attempts before logging a warning. Helps identify persistent bootstrapping issues while avoiding log spam for temporary problems. |

### Chain Configuration

Configuration for blockchain-specific settings. For detailed chain-specific configuration options (C-Chain, P-Chain, X-Chain, Subnet-EVM), see the [Chain Configs documentation](/docs/nodes/chain-configs).

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--chain-config-dir` | `AVAGO_CHAIN_CONFIG_DIR` | string | `~/.avalanchego/configs/chains` | Directory for chain-specific configurations. Structure: `{chain-config-dir}/{chainID}/config.*` See [Chain Configs documentation](/docs/nodes/chain-configs) for detailed configuration options |
| `--chain-config-content` | `AVAGO_CHAIN_CONFIG_CONTENT` | string | - | Base64 encoded chain config as alternative to config files. See [Chain Configs documentation](/docs/nodes/chain-configs) |
| `--chain-aliases-file` | `AVAGO_CHAIN_ALIASES_FILE` | string | `~/.avalanchego/configs/chains/aliases.json` | JSON file mapping blockchain IDs to aliases. Example: `{"q2aTwKuyzgs8pynF7UXBZCU7DejbZbZ6EUyHr3JQzYgwNPUPi": ["DFK"]}` |
| `--chain-aliases-file-content` | `AVAGO_CHAIN_ALIASES_FILE_CONTENT` | string | - | Base64 encoded chain aliases |
| `--chain-data-dir` | `AVAGO_CHAIN_DATA_DIR` | string | `~/.avalanchego/chainData` | Chain data directory |

### Config File

Node configuration file settings.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--config-file` | `AVAGO_CONFIG_FILE` | string | - | JSON config file path |
| `--config-file-content` | `AVAGO_CONFIG_FILE_CONTENT` | string | - | Base64 encoded config content |
| `--config-file-content-type` | `AVAGO_CONFIG_FILE_CONTENT_TYPE` | string | `JSON` | Config content format (JSON, TOML, YAML) |

### Data Directory

Base directory configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--data-dir` | `AVAGO_DATA_DIR` | string | `~/.avalanchego` | Base data directory |

### Database

Database configuration options.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--db-dir` | `AVAGO_DB_DIR` | string | `~/.avalanchego/db` | Database directory |
| `--db-type` | `AVAGO_DB_TYPE` | string | `leveldb` | Database type (leveldb, memdb, pebbledb) |
| `--db-config-file` | `AVAGO_DB_CONFIG_FILE` | string | - | Database config file path |
| `--db-config-file-content` | `AVAGO_DB_CONFIG_FILE_CONTENT` | string | - | Base64 encoded database config |

### File Descriptor Limit

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--fd-limit` | `AVAGO_FD_LIMIT` | int | `32768` | Target file descriptor limit |

### Genesis

Genesis configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--genesis-file` | `AVAGO_GENESIS_FILE` | string | - | Genesis file path |
| `--genesis-file-content` | `AVAGO_GENESIS_FILE_CONTENT` | string | - | Base64 encoded genesis data |

### HTTP Server

HTTP API server configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--http-allowed-hosts` | `AVAGO_HTTP_ALLOWED_HOSTS` | string | `localhost` | Allowed hostnames (comma-separated) |
| `--http-allowed-origins` | `AVAGO_HTTP_ALLOWED_ORIGINS` | string | `*` | Allowed origins |
| `--http-host` | `AVAGO_HTTP_HOST` | string | `127.0.0.1` | HTTP server listen address |
| `--http-port` | `AVAGO_HTTP_PORT` | int | `9650` | HTTP server port |
| `--http-idle-timeout` | `AVAGO_HTTP_IDLE_TIMEOUT` | duration | - | Keep-alive timeout |
| `--http-read-timeout` | `AVAGO_HTTP_READ_TIMEOUT` | duration | - | Request read timeout |
| `--http-read-header-timeout` | `AVAGO_HTTP_READ_HEADER_TIMEOUT` | duration | - | Header read timeout |
| `--http-write-timeout` | `AVAGO_HTTP_WRITE_TIMEOUT` | duration | - | Response write timeout |
| `--http-shutdown-timeout` | `AVAGO_HTTP_SHUTDOWN_TIMEOUT` | duration | `10s` | Graceful shutdown timeout |
| `--http-shutdown-wait` | `AVAGO_HTTP_SHUTDOWN_WAIT` | duration | `0s` | Wait before shutdown |
| `--http-tls-enabled` | `AVAGO_HTTP_TLS_ENABLED` | boolean | `false` | Enable HTTPS |
| `--http-tls-cert-file` | `AVAGO_HTTP_TLS_CERT_FILE` | string | - | TLS certificate file |
| `--http-tls-cert-file-content` | `AVAGO_HTTP_TLS_CERT_FILE_CONTENT` | string | - | Base64 encoded TLS certificate |
| `--http-tls-key-file` | `AVAGO_HTTP_TLS_KEY_FILE` | string | - | TLS key file |
| `--http-tls-key-file-content` | `AVAGO_HTTP_TLS_KEY_FILE_CONTENT` | string | - | Base64 encoded TLS key |

### Logging

Logging configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--log-level` | `AVAGO_LOG_LEVEL` | string | `info` | Log level (off, fatal, error, warn, info, trace, debug, verbo) |
| `--log-display-level` | `AVAGO_LOG_DISPLAY_LEVEL` | string | value of `--log-level` | Console log level |
| `--log-format` | `AVAGO_LOG_FORMAT` | string | `auto` | Log format (auto, plain, colors, json) |
| `--log-dir` | `AVAGO_LOG_DIR` | string | `~/.avalanchego/logs` | Log directory |
| `--log-disable-display-plugin-logs` | `AVAGO_LOG_DISABLE_DISPLAY_PLUGIN_LOGS` | boolean | `false` | Hide plugin logs from stdout |
| `--log-rotater-max-size` | `AVAGO_LOG_ROTATER_MAX_SIZE` | uint | `8` | Max log file size (MB) |
| `--log-rotater-max-files` | `AVAGO_LOG_ROTATER_MAX_FILES` | uint | `7` | Max rotated files to keep |
| `--log-rotater-max-age` | `AVAGO_LOG_ROTATER_MAX_AGE` | uint | `0` | Max days to keep rotated files |
| `--log-rotater-compress-enabled` | `AVAGO_LOG_ROTATER_COMPRESS_ENABLED` | boolean | `false` | Compress rotated files |

### Network

Network identification and configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--network-id` | `AVAGO_NETWORK_ID` | string | `mainnet` | Network to connect to (mainnet, fuji, testnet, local, network-{id}) |

### OpenTelemetry

Tracing and observability configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--tracing-endpoint` | `AVAGO_TRACING_ENDPOINT` | string | See description | Trace export endpoint |
| `--tracing-exporter-type` | `AVAGO_TRACING_EXPORTER_TYPE` | string | `disabled` | Exporter type (disabled, grpc, http) |
| `--tracing-insecure` | `AVAGO_TRACING_INSECURE` | boolean | `true` | Skip TLS for tracing |
| `--tracing-sample-rate` | `AVAGO_TRACING_SAMPLE_RATE` | float | `0.1` | Trace sampling rate |

### Partial Sync

Partial synchronization settings.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--partial-sync-primary-network` | `AVAGO_PARTIAL_SYNC_PRIMARY_NETWORK` | boolean | `false` | Sync only P-chain on primary network |

### Public IP

Public IP configuration for validators.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--public-ip` | `AVAGO_PUBLIC_IP` | string | - | Node's public IP address |
| `--public-ip-resolution-frequency` | `AVAGO_PUBLIC_IP_RESOLUTION_FREQUENCY` | duration | `5m` | IP resolution frequency |
| `--public-ip-resolution-service` | `AVAGO_PUBLIC_IP_RESOLUTION_SERVICE` | string | - | IP resolution service (ifconfigCo, opendns, ifconfigMe) |

### State Sync

State synchronization configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--state-sync-ids` | `AVAGO_STATE_SYNC_IDS` | string | - | Comma-separated validator IDs for state sync |
| `--state-sync-ips` | `AVAGO_STATE_SYNC_IPS` | string | - | Comma-separated IP:port pairs for state sync |

### Staking

Staking and validator configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--staking-port` | `AVAGO_STAKING_PORT` | int | `9651` | Staking port |
| `--sybil-protection-enabled` | `AVAGO_SYBIL_PROTECTION_ENABLED` | boolean | `true` | Enable sybil protection (PoS) |
| `--sybil-protection-disabled-weight` | `AVAGO_SYBIL_PROTECTION_DISABLED_WEIGHT` | uint | `100` | Weight when sybil protection disabled |
| `--staking-tls-cert-file` | `AVAGO_STAKING_TLS_CERT_FILE` | string | `~/.avalanchego/staking/staker.crt` | Staking TLS certificate |
| `--staking-tls-cert-file-content` | `AVAGO_STAKING_TLS_CERT_FILE_CONTENT` | string | - | Base64 encoded staking certificate |
| `--staking-tls-key-file` | `AVAGO_STAKING_TLS_KEY_FILE` | string | `~/.avalanchego/staking/staker.key` | Staking TLS key |
| `--staking-tls-key-file-content` | `AVAGO_STAKING_TLS_KEY_FILE_CONTENT` | string | - | Base64 encoded staking key |

### Subnets

Subnet configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--track-subnets` | `AVAGO_TRACK_SUBNETS` | string | - | Comma-separated subnet IDs to track |
| `--subnet-config-dir` | `AVAGO_SUBNET_CONFIG_DIR` | string | `~/.avalanchego/configs/subnets` | Subnet config directory |
| `--subnet-config-content` | `AVAGO_SUBNET_CONFIG_CONTENT` | string | - | Base64 encoded subnet config |

### Version

Version information.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--version` | `AVAGO_VERSION` | boolean | `false` | Print version and exit |

## Advanced Options

⚠️ **Warning**: The following options may affect node correctness. Only modify if you understand the implications.

### Gossiping

Consensus gossiping parameters.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--consensus-accepted-frontier-gossip-validator-size` | `AVAGO_CONSENSUS_ACCEPTED_FRONTIER_GOSSIP_VALIDATOR_SIZE` | uint | `0` | Validators to gossip accepted frontier |
| `--consensus-accepted-frontier-gossip-non-validator-size` | `AVAGO_CONSENSUS_ACCEPTED_FRONTIER_GOSSIP_NON_VALIDATOR_SIZE` | uint | `0` | Non-validators to gossip accepted frontier |
| `--consensus-accepted-frontier-gossip-peer-size` | `AVAGO_CONSENSUS_ACCEPTED_FRONTIER_GOSSIP_PEER_SIZE` | uint | `15` | Peers to gossip accepted frontier |
| `--consensus-accepted-frontier-gossip-frequency` | `AVAGO_CONSENSUS_ACCEPTED_FRONTIER_GOSSIP_FREQUENCY` | duration | `10s` | Accepted frontier gossip frequency |
| `--consensus-on-accept-gossip-validator-size` | `AVAGO_CONSENSUS_ON_ACCEPT_GOSSIP_VALIDATOR_SIZE` | uint | `0` | Validators to gossip on accept |
| `--consensus-on-accept-gossip-non-validator-size` | `AVAGO_CONSENSUS_ON_ACCEPT_GOSSIP_NON_VALIDATOR_SIZE` | uint | `0` | Non-validators to gossip on accept |
| `--consensus-on-accept-gossip-peer-size` | `AVAGO_CONSENSUS_ON_ACCEPT_GOSSIP_PEER_SIZE` | uint | `10` | Peers to gossip on accept |

### Benchlist

Peer benchlisting configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--benchlist-duration` | `AVAGO_BENCHLIST_DURATION` | duration | `15m` | Max benchlist duration |
| `--benchlist-fail-threshold` | `AVAGO_BENCHLIST_FAIL_THRESHOLD` | int | `10` | Failures before benchlisting |
| `--benchlist-min-failing-duration` | `AVAGO_BENCHLIST_MIN_FAILING_DURATION` | duration | `150s` | Min failing duration before benchlist |

### Consensus Parameters

Core consensus configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--consensus-shutdown-timeout` | `AVAGO_CONSENSUS_SHUTDOWN_TIMEOUT` | duration | `5s` | Unresponsive chain timeout |
| `--create-asset-tx-fee` | `AVAGO_CREATE_ASSET_TX_FEE` | int | `10000000` | Asset creation fee (nAVAX) |
| `--tx-fee` | `AVAGO_TX_FEE` | int | `1000000` | Transaction fee (nAVAX) |
| `--uptime-requirement` | `AVAGO_UPTIME_REQUIREMENT` | float | `0.8` | Required uptime for rewards |
| `--uptime-metric-freq` | `AVAGO_UPTIME_METRIC_FREQ` | duration | `30s` | Uptime metric update frequency |

### Staking Parameters

Staking economics configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--min-validator-stake` | `AVAGO_MIN_VALIDATOR_STAKE` | int | network dependent | Min validator stake (nAVAX) |
| `--max-validator-stake` | `AVAGO_MAX_VALIDATOR_STAKE` | int | network dependent | Max validator stake (nAVAX) |
| `--min-delegator-stake` | `AVAGO_MIN_DELEGATOR_STAKE` | int | network dependent | Min delegator stake (nAVAX) |
| `--min-delegation-fee` | `AVAGO_MIN_DELEGATION_FEE` | int | `20000` | Min delegation fee (x10,000) |
| `--min-stake-duration` | `AVAGO_MIN_STAKE_DURATION` | duration | `336h` | Min staking duration |
| `--max-stake-duration` | `AVAGO_MAX_STAKE_DURATION` | duration | `8760h` | Max staking duration |
| `--stake-minting-period` | `AVAGO_STAKE_MINTING_PERIOD` | duration | `8760h` | Stake minting period |
| `--stake-max-consumption-rate` | `AVAGO_STAKE_MAX_CONSUMPTION_RATE` | uint | `120000` | Max consumption rate |
| `--stake-min-consumption-rate` | `AVAGO_STAKE_MIN_CONSUMPTION_RATE` | uint | `100000` | Min consumption rate |
| `--stake-supply-cap` | `AVAGO_STAKE_SUPPLY_CAP` | uint | `720000000000000000` | Max stake supply (nAVAX) |

### Snow Consensus

Snow consensus protocol parameters.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--snow-concurrent-repolls` | `AVAGO_SNOW_CONCURRENT_REPOLLS` | int | `4` | Concurrent repolls |
| `--snow-sample-size` | `AVAGO_SNOW_SAMPLE_SIZE` | int | `20` | Sample size (k) |
| `--snow-quorum-size` | `AVAGO_SNOW_QUORUM_SIZE` | int | `15` | Quorum size (alpha) |
| `--snow-commit-threshold` | `AVAGO_SNOW_COMMIT_THRESHOLD` | int | `20` | Commit threshold (beta) |
| `--snow-optimal-processing` | `AVAGO_SNOW_OPTIMAL_PROCESSING` | int | `50` | Optimal processing items |
| `--snow-max-processing` | `AVAGO_SNOW_MAX_PROCESSING` | int | `1024` | Max processing items |
| `--snow-max-time-processing` | `AVAGO_SNOW_MAX_TIME_PROCESSING` | duration | `2m` | Max processing time |

### ProposerVM

ProposerVM configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--proposervm-use-current-height` | `AVAGO_PROPOSERVM_USE_CURRENT_HEIGHT` | boolean | `false` | Report current P-chain height |
| `--proposervm-min-block-delay` | `AVAGO_PROPOSERVM_MIN_BLOCK_DELAY` | duration | `1s` | Min block build delay |

### Continuous Profiling

Performance profiling configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--profile-continuous-enabled` | `AVAGO_PROFILE_CONTINUOUS_ENABLED` | boolean | `false` | Enable continuous profiling |
| `--profile-dir` | `AVAGO_PROFILE_DIR` | string | `~/.avalanchego/profiles/` | Profile output directory |
| `--profile-continuous-freq` | `AVAGO_PROFILE_CONTINUOUS_FREQ` | duration | `15m` | Profile creation frequency |
| `--profile-continuous-max-files` | `AVAGO_PROFILE_CONTINUOUS_MAX_FILES` | int | `5` | Max profile files to keep |

### Health Checks

Health monitoring configuration.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--health-check-frequency` | `AVAGO_HEALTH_CHECK_FREQUENCY` | duration | `30s` | Health check frequency |
| `--health-check-averager-halflife` | `AVAGO_HEALTH_CHECK_AVERAGER_HALFLIFE` | duration | `10s` | Averager half-life |

### Network Configuration

Advanced network settings.

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--network-allow-private-ips` | `AVAGO_NETWORK_ALLOW_PRIVATE_IPS` | boolean | `true` | Allow private IP connections |
| `--network-compression-type` | `AVAGO_NETWORK_COMPRESSION_TYPE` | string | `gzip` | Message compression (gzip, zstd, none) |
| `--network-initial-timeout` | `AVAGO_NETWORK_INITIAL_TIMEOUT` | duration | `5s` | Initial timeout |
| `--network-initial-reconnect-delay` | `AVAGO_NETWORK_INITIAL_RECONNECT_DELAY` | duration | `1s` | Initial reconnect delay |
| `--network-max-reconnect-delay` | `AVAGO_NETWORK_MAX_RECONNECT_DELAY` | duration | `1h` | Max reconnect delay |
| `--network-minimum-timeout` | `AVAGO_NETWORK_MINIMUM_TIMEOUT` | duration | `2s` | Min adaptive timeout |
| `--network-maximum-timeout` | `AVAGO_NETWORK_MAXIMUM_TIMEOUT` | duration | `10s` | Max adaptive timeout |
| `--network-maximum-inbound-timeout` | `AVAGO_NETWORK_MAXIMUM_INBOUND_TIMEOUT` | duration | `10s` | Max inbound message timeout |
| `--network-timeout-halflife` | `AVAGO_NETWORK_TIMEOUT_HALFLIFE` | duration | `5m` | Timeout halflife |
| `--network-timeout-coefficient` | `AVAGO_NETWORK_TIMEOUT_COEFFICIENT` | float | `2` | Timeout coefficient |
| `--network-read-handshake-timeout` | `AVAGO_NETWORK_READ_HANDSHAKE_TIMEOUT` | duration | `15s` | Handshake read timeout |
| `--network-ping-timeout` | `AVAGO_NETWORK_PING_TIMEOUT` | duration | `30s` | Ping timeout |
| `--network-ping-frequency` | `AVAGO_NETWORK_PING_FREQUENCY` | duration | `22.5s` | Ping frequency |
| `--network-health-min-conn-peers` | `AVAGO_NETWORK_HEALTH_MIN_CONN_PEERS` | uint | `1` | Min healthy connections |
| `--network-health-max-time-since-msg-received` | `AVAGO_NETWORK_HEALTH_MAX_TIME_SINCE_MSG_RECEIVED` | duration | `1m` | Max time since message received |
| `--network-health-max-time-since-msg-sent` | `AVAGO_NETWORK_HEALTH_MAX_TIME_SINCE_MSG_SENT` | duration | `1m` | Max time since message sent |
| `--network-health-max-portion-send-queue-full` | `AVAGO_NETWORK_HEALTH_MAX_PORTION_SEND_QUEUE_FULL` | float | `0.9` | Max send queue portion |
| `--network-health-max-send-fail-rate` | `AVAGO_NETWORK_HEALTH_MAX_SEND_FAIL_RATE` | float | `0.25` | Max send failure rate |
| `--network-health-max-outstanding-request-duration` | `AVAGO_NETWORK_HEALTH_MAX_OUTSTANDING_REQUEST_DURATION` | duration | `5m` | Max outstanding request duration |
| `--network-max-clock-difference` | `AVAGO_NETWORK_MAX_CLOCK_DIFFERENCE` | duration | `1m` | Max clock difference |
| `--network-require-validator-to-connect` | `AVAGO_NETWORK_REQUIRE_VALIDATOR_TO_CONNECT` | boolean | `false` | Require validator connections |
| `--network-tcp-proxy-enabled` | `AVAGO_NETWORK_TCP_PROXY_ENABLED` | boolean | `false` | Require TCP proxy header |
| `--network-tcp-proxy-read-timeout` | `AVAGO_NETWORK_TCP_PROXY_READ_TIMEOUT` | duration | `3s` | TCP proxy read timeout |
| `--network-outbound-connection-timeout` | `AVAGO_NETWORK_OUTBOUND_CONNECTION_TIMEOUT` | duration | `30s` | Outbound connection timeout |

### Message Rate-Limiting

#### CPU Based Rate-Limiting

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--throttler-inbound-cpu-validator-alloc` | `AVAGO_THROTTLER_INBOUND_CPU_VALIDATOR_ALLOC` | float | half of CPUs | CPU allocation for validators |
| `--throttler-inbound-cpu-max-recheck-delay` | `AVAGO_THROTTLER_INBOUND_CPU_MAX_RECHECK_DELAY` | duration | `5s` | CPU recheck delay |
| `--throttler-inbound-disk-max-recheck-delay` | `AVAGO_THROTTLER_INBOUND_DISK_MAX_RECHECK_DELAY` | duration | `5s` | Disk recheck delay |
| `--throttler-inbound-cpu-max-non-validator-usage` | `AVAGO_THROTTLER_INBOUND_CPU_MAX_NON_VALIDATOR_USAGE` | float | 80% of CPUs | Max non-validator CPU usage |
| `--throttler-inbound-cpu-max-non-validator-node-usage` | `AVAGO_THROTTLER_INBOUND_CPU_MAX_NON_VALIDATOR_NODE_USAGE` | float | CPUs / 8 | Max per-node CPU usage |
| `--throttler-inbound-disk-validator-alloc` | `AVAGO_THROTTLER_INBOUND_DISK_VALIDATOR_ALLOC` | float | `1000 GiB/s` | Disk allocation for validators |
| `--throttler-inbound-disk-max-non-validator-usage` | `AVAGO_THROTTLER_INBOUND_DISK_MAX_NON_VALIDATOR_USAGE` | float | `1000 GiB/s` | Max non-validator disk usage |
| `--throttler-inbound-disk-max-non-validator-node-usage` | `AVAGO_THROTTLER_INBOUND_DISK_MAX_NON_VALIDATOR_NODE_USAGE` | float | `1000 GiB/s` | Max per-node disk usage |

#### Bandwidth Based Rate-Limiting

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--throttler-inbound-bandwidth-refill-rate` | `AVAGO_THROTTLER_INBOUND_BANDWIDTH_REFILL_RATE` | uint | `512` | Bandwidth refill rate (bytes/sec) |
| `--throttler-inbound-bandwidth-max-burst-size` | `AVAGO_THROTTLER_INBOUND_BANDWIDTH_MAX_BURST_SIZE` | uint | `2 MiB` | Max bandwidth burst |

#### Message Size Based Rate-Limiting

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--throttler-inbound-at-large-alloc-size` | `AVAGO_THROTTLER_INBOUND_AT_LARGE_ALLOC_SIZE` | uint | `6 MiB` | At-large allocation size |
| `--throttler-inbound-validator-alloc-size` | `AVAGO_THROTTLER_INBOUND_VALIDATOR_ALLOC_SIZE` | uint | `32 MiB` | Validator allocation size |
| `--throttler-inbound-node-max-at-large-bytes` | `AVAGO_THROTTLER_INBOUND_NODE_MAX_AT_LARGE_BYTES` | uint | `2 MiB` | Max at-large bytes per node |

#### Message Based Rate-Limiting

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--throttler-inbound-node-max-processing-msgs` | `AVAGO_THROTTLER_INBOUND_NODE_MAX_PROCESSING_MSGS` | uint | `1024` | Max processing messages per node |

#### Outbound Rate-Limiting

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--throttler-outbound-at-large-alloc-size` | `AVAGO_THROTTLER_OUTBOUND_AT_LARGE_ALLOC_SIZE` | uint | `32 MiB` | Outbound at-large allocation |
| `--throttler-outbound-validator-alloc-size` | `AVAGO_THROTTLER_OUTBOUND_VALIDATOR_ALLOC_SIZE` | uint | `32 MiB` | Outbound validator allocation |
| `--throttler-outbound-node-max-at-large-bytes` | `AVAGO_THROTTLER_OUTBOUND_NODE_MAX_AT_LARGE_BYTES` | uint | `2 MiB` | Max outbound at-large bytes |

### Connection Rate-Limiting

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--network-inbound-connection-throttling-cooldown` | `AVAGO_NETWORK_INBOUND_CONNECTION_THROTTLING_COOLDOWN` | duration | `10s` | Connection upgrade cooldown |
| `--network-inbound-connection-throttling-max-conns-per-sec` | `AVAGO_NETWORK_INBOUND_CONNECTION_THROTTLING_MAX_CONNS_PER_SEC` | uint | `512` | Max inbound connections/sec |
| `--network-outbound-connection-throttling-rps` | `AVAGO_NETWORK_OUTBOUND_CONNECTION_THROTTLING_RPS` | uint | `50` | Outbound connection attempts/sec |

### Peer List Gossiping

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--network-peer-list-num-validator-ips` | `AVAGO_NETWORK_PEER_LIST_NUM_VALIDATOR_IPS` | int | `15` | Validator IPs to gossip |
| `--network-peer-list-validator-gossip-size` | `AVAGO_NETWORK_PEER_LIST_VALIDATOR_GOSSIP_SIZE` | int | `20` | Validators to gossip to |
| `--network-peer-list-non-validator-gossip-size` | `AVAGO_NETWORK_PEER_LIST_NON_VALIDATOR_GOSSIP_SIZE` | int | `0` | Non-validators to gossip to |
| `--network-peer-list-peers-gossip-size` | `AVAGO_NETWORK_PEER_LIST_PEERS_GOSSIP_SIZE` | int | `0` | Total peers to gossip to |
| `--network-peer-list-gossip-frequency` | `AVAGO_NETWORK_PEER_LIST_GOSSIP_FREQUENCY` | duration | `1m` | Gossip frequency |
| `--network-peer-read-buffer-size` | `AVAGO_NETWORK_PEER_READ_BUFFER_SIZE` | int | `8 KiB` | Peer read buffer size |
| `--network-peer-write-buffer-size` | `AVAGO_NETWORK_PEER_WRITE_BUFFER_SIZE` | int | `8 KiB` | Peer write buffer size |

### Resource Usage Tracking

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--meter-vms-enabled` | `AVAGO_METER_VMS_ENABLED` | boolean | `true` | Enable VM metering |
| `--system-tracker-frequency` | `AVAGO_SYSTEM_TRACKER_FREQUENCY` | duration | `500ms` | System tracking frequency |
| `--system-tracker-processing-halflife` | `AVAGO_SYSTEM_TRACKER_PROCESSING_HALFLIFE` | duration | `15s` | Processing tracker halflife |
| `--system-tracker-cpu-halflife` | `AVAGO_SYSTEM_TRACKER_CPU_HALFLIFE` | duration | `15s` | CPU tracker halflife |
| `--system-tracker-disk-halflife` | `AVAGO_SYSTEM_TRACKER_DISK_HALFLIFE` | duration | `1m` | Disk tracker halflife |
| `--system-tracker-disk-required-available-space` | `AVAGO_SYSTEM_TRACKER_DISK_REQUIRED_AVAILABLE_SPACE` | uint | `512MiB` | Required disk space |
| `--system-tracker-disk-warning-threshold-available-space` | `AVAGO_SYSTEM_TRACKER_DISK_WARNING_THRESHOLD_AVAILABLE_SPACE` | uint | `1GiB` | Disk space warning threshold |

### Additional Configuration

| Flag | Environment Variable | Type | Default | Description |
|------|---------------------|------|---------|-------------|
| `--plugin-dir` | `AVAGO_PLUGIN_DIR` | string | `~/.avalanchego/plugins` | VM plugins directory |
| `--vm-aliases-file` | `AVAGO_VM_ALIASES_FILE` | string | `~/.avalanchego/configs/vms/aliases.json` | VM aliases file |
| `--vm-aliases-file-content` | `AVAGO_VM_ALIASES_FILE_CONTENT` | string | - | Base64 encoded VM aliases |
| `--index-allow-incomplete` | `AVAGO_INDEX_ALLOW_INCOMPLETE` | boolean | `false` | Allow incomplete index |
| `--router-health-max-drop-rate` | `AVAGO_ROUTER_HEALTH_MAX_DROP_RATE` | float | `1` | Max healthy message drop rate |
| `--router-health-max-outstanding-requests` | `AVAGO_ROUTER_HEALTH_MAX_OUTSTANDING_REQUESTS` | uint | `1024` | Max outstanding requests |

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

## Additional Resources

- [Full documentation](https://docs.avax.network/)
- [Example configurations](https://github.com/ava-labs/avalanchego/tree/master/config)
- [Network upgrade schedules](https://docs.avax.network/learn/avalanche/avalanche-platform)

