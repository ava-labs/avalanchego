# Subnet-EVM Configuration

> **Note**: These are the configuration options available in the Subnet-EVM codebase. To set these values, you need to create a configuration file at `~/.avalanchego/configs/chains/<blockchainID>/config.json`.
>
> For the AvalancheGo node configuration options, see the AvalancheGo Configuration page.

This document describes all configuration options available for Subnet-EVM.

## Example Configuration

```json
{
  "eth-apis": ["eth", "eth-filter", "net", "web3"],
  "pruning-enabled": true,
  "commit-interval": 4096,
  "trie-clean-cache": 512,
  "trie-dirty-cache": 512,
  "snapshot-cache": 256,
  "rpc-gas-cap": 50000000,
  "log-level": "info",
  "metrics-expensive-enabled": true,
  "continuous-profiler-dir": "./profiles",
  "state-sync-enabled": false,
  "accepted-cache-size": 32
}
```

## Configuration Format

Configuration is provided as a JSON object. All fields are optional unless otherwise specified.

## API Configuration

### Ethereum APIs

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `eth-apis` | array of strings | List of Ethereum services that should be enabled | `["eth", "eth-filter", "net", "web3", "internal-eth", "internal-blockchain", "internal-transaction"]` |

### Subnet-EVM Specific APIs

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `validators-api-enabled` | bool | Enable the validators API | `true` |
| `admin-api-enabled` | bool | Enable the admin API for administrative operations | `false` |
| `admin-api-dir` | string | Directory for admin API operations | - |
| `warp-api-enabled` | bool | Enable the Warp API for cross-chain messaging | `false` |

### API Limits and Security

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `rpc-gas-cap` | uint64 | Maximum gas limit for RPC calls | `50,000,000` |
| `rpc-tx-fee-cap` | float64 | Maximum transaction fee cap in AVAX | `100` |
| `api-max-duration` | duration | Maximum duration for API calls (0 = no limit) | `0` |
| `api-max-blocks-per-request` | int64 | Maximum number of blocks per getLogs request (0 = no limit) | `0` |
| `http-body-limit` | uint64 | Maximum size of HTTP request bodies | - |
| `batch-request-limit` | uint64 | Maximum number of requests that can be batched in an RPC call. For no limit, set either this or `batch-response-max-size` to 0 | `1000` | 
| `batch-response-max-size` | uint64 | Maximum size (in bytes) of response that can be returned from a batched RPC call. For no limit, set either this or `batch-request-limit` to 0. Defaults to `25 MB`| `1000` |

### WebSocket Settings

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `ws-cpu-refill-rate` | duration | Rate at which WebSocket CPU usage quota is refilled (0 = no limit) | `0` |
| `ws-cpu-max-stored` | duration | Maximum stored WebSocket CPU usage quota (0 = no limit) | `0` |

## Cache Configuration

### Trie Caches

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `trie-clean-cache` | int | Size of the trie clean cache in MB | `512` |
| `trie-dirty-cache` | int | Size of the trie dirty cache in MB | `512` |
| `trie-dirty-commit-target` | int | Memory limit to target in the dirty cache before performing a commit in MB | `20` |
| `trie-prefetcher-parallelism` | int | Maximum concurrent disk reads trie prefetcher should perform | `16` |

### Other Caches

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `snapshot-cache` | int | Size of the snapshot disk layer clean cache in MB | `256` |
| `accepted-cache-size` | int | Depth to keep in the accepted headers and logs cache (blocks) | `32` |
| `state-sync-server-trie-cache` | int | Trie cache size for state sync server in MB | `64` |

## Ethereum Settings

### Transaction Processing

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `preimages-enabled` | bool | Enable preimage recording | `false` |
| `allow-unfinalized-queries` | bool | Allow queries for unfinalized blocks | `false` |
| `allow-unprotected-txs` | bool | Allow unprotected transactions (without EIP-155) | `false` |
| `allow-unprotected-tx-hashes` | array | List of specific transaction hashes allowed to be unprotected | EIP-1820 registry tx |
| `local-txs-enabled` | bool | Enable treatment of transactions from local accounts as local | `false` |

### Snapshots

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `snapshot-wait` | bool | Wait for snapshot generation on startup | `false` |
| `snapshot-verification-enabled` | bool | Enable snapshot verification | `false` |

## Pruning and State Management

### Basic Pruning

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `pruning-enabled` | bool | Enable state pruning to save disk space | `true` |
| `commit-interval` | uint64 | Interval at which to persist EVM and atomic tries (blocks) | `4096` |
| `accepted-queue-limit` | int | Maximum blocks to queue before blocking during acceptance | `64` |

### State Reconstruction

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `allow-missing-tries` | bool | Suppress warnings about incomplete trie index | `false` |
| `populate-missing-tries` | uint64 | Starting block for re-populating missing tries (null = disabled) | `null` |
| `populate-missing-tries-parallelism` | int | Concurrent readers for re-populating missing tries | `1024` |

### Offline Pruning

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `offline-pruning-enabled` | bool | Enable offline pruning | `false` |
| `offline-pruning-bloom-filter-size` | uint64 | Bloom filter size for offline pruning in MB | `512` |
| `offline-pruning-data-directory` | string | Directory for offline pruning data | - |

### Historical Data

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `historical-proof-query-window` | uint64 | Number of blocks before last accepted for proof queries (archive mode only, ~24 hours) | `43200` |
| `state-history` | uint64 | Number of most recent states that are accesible on disk (pruning mode only) | `32` |

## Transaction Pool Configuration

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `tx-pool-price-limit` | uint64 | Minimum gas price for transaction acceptance | - |
| `tx-pool-price-bump` | uint64 | Minimum price bump percentage for transaction replacement | - |
| `tx-pool-account-slots` | uint64 | Maximum number of executable transaction slots per account | - |
| `tx-pool-global-slots` | uint64 | Maximum number of executable transaction slots for all accounts | - |
| `tx-pool-account-queue` | uint64 | Maximum number of non-executable transaction slots per account | - |
| `tx-pool-global-queue` | uint64 | Maximum number of non-executable transaction slots for all accounts | - |
| `tx-pool-lifetime` | duration | Maximum time transactions can stay in the pool | - |

## Gossip Configuration

### Push Gossip Settings

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `push-gossip-percent-stake` | float64 | Percentage of total stake to push gossip to (range: [0, 1]) | `0.9` |
| `push-gossip-num-validators` | int | Number of validators to push gossip to | `100` |
| `push-gossip-num-peers` | int | Number of non-validator peers to push gossip to | `0` |

### Regossip Settings

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `push-regossip-num-validators` | int | Number of validators to regossip to | `10` |
| `push-regossip-num-peers` | int | Number of non-validator peers to regossip to | `0` |
| `priority-regossip-addresses` | array | Addresses to prioritize for regossip | - |

### Timing Configuration

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `push-gossip-frequency` | duration | Frequency of push gossip | `100ms` |
| `pull-gossip-frequency` | duration | Frequency of pull gossip | `1s` |
| `regossip-frequency` | duration | Frequency of regossip | `30s` |

## Logging and Monitoring

### Logging

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `log-level` | string | Logging level (trace, debug, info, warn, error, crit) | `"info"` |
| `log-json-format` | bool | Use JSON format for logs | `false` |

### Profiling

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `continuous-profiler-dir` | string | Directory for continuous profiler output (empty = disabled) | - |
| `continuous-profiler-frequency` | duration | Frequency to run continuous profiler | `15m` |
| `continuous-profiler-max-files` | int | Maximum number of profiler files to maintain | `5` |

### Metrics

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `metrics-expensive-enabled` | bool | Enable expensive debug-level metrics; this includes Firewood metrics | `true` |

## Security and Access

### Keystore

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `keystore-directory` | string | Directory for keystore files (absolute or relative path) | - |
| `keystore-external-signer` | string | External signer configuration | - |
| `keystore-insecure-unlock-allowed` | bool | Allow insecure account unlocking | `false` |

### Fee Configuration

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `feeRecipient` | string | Address to send transaction fees to (leave empty if not supported) | - |

## Network and Sync

### Network

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `max-outbound-active-requests` | int64 | Maximum number of outbound active requests for VM2VM network | `16` |

### State Sync

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `state-sync-enabled` | bool | Enable state sync | `false` |
| `state-sync-skip-resume` | bool | Force state sync to use highest available summary block | `false` |
| `state-sync-ids` | string | Comma-separated list of state sync IDs | - |
| `state-sync-commit-interval` | uint64 | Commit interval for state sync (blocks) | `16384` |
| `state-sync-min-blocks` | uint64 | Minimum blocks ahead required for state sync | `300000` |
| `state-sync-request-size` | uint16 | Number of key/values to request per state sync request | `1024` |

## Database Configuration

> **WARNING**: `firewood` and `path` schemes are untested in production. Using `path` is strongly discouraged. To use `firewood`, you must also set the following config options:
>
> - `pruning-enabled: true` (enabled by default)
> - `state-sync-enabled: false`
> - `snapshot-cache: 0`

Failing to set these options will result in errors on VM initialization. Additionally, not all APIs are available - see these portions of the config documentation for more details.

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `database-type` | string | Type of database to use | `"pebbledb"` |
| `database-path` | string | Path to database directory | - |
| `database-read-only` | bool | Open database in read-only mode | `false` |
| `database-config` | string | Inline database configuration | - |
| `database-config-file` | string | Path to database configuration file | - |
| `use-standalone-database` | bool | Use standalone database instead of shared one | - |
| `inspect-database` | bool | Inspect database on startup | `false` |
| `state-scheme` | string |  EXPERIMENTAL: specifies the database scheme to store state data; can be one of `hash` or `firewood` | `hash` | 

## Transaction Indexing

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `transaction-history` | uint64 | Maximum number of blocks from head whose transaction indices are reserved (0 = no limit) | - |
| `tx-lookup-limit` | uint64 | **Deprecated** - use `transaction-history` instead | - |
| `skip-tx-indexing` | bool | Skip indexing transactions entirely | `false` |

## Warp Configuration

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `warp-off-chain-messages` | array | Off-chain messages the node should be willing to sign | - |
| `prune-warp-db-enabled` | bool | Clear warp database on startup | `false` |

## Miscellaneous

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `airdrop` | string | Path to airdrop file | - |
| `skip-upgrade-check` | bool | Skip checking that upgrades occur before last accepted block ⚠️ **Warning**: Only use when you understand the implications | `false` |
| `min-delay-target` | integer | The minimum delay between blocks (in milliseconds) that this node will attempt to use when creating blocks | Parent block's target |

## Gossip Constants

The following constants are defined for transaction gossip behavior and cannot be configured without a custom build of Subnet-EVM:

| Constant | Type | Description | Value |
|----------|------|-------------|-------|
| Bloom Filter Min Target Elements | int | Minimum target elements for bloom filter | `8,192` |
| Bloom Filter Target False Positive Rate | float | Target false positive rate | `1%` |
| Bloom Filter Reset False Positive Rate | float | Reset false positive rate | `5%` |
| Bloom Filter Churn Multiplier | int | Churn multiplier | `3` |
| Push Gossip Discarded Elements | int | Number of discarded elements | `16,384` |
| Tx Gossip Target Message Size | size | Target message size for transaction gossip | `20 KiB` |
| Tx Gossip Throttling Period | duration | Throttling period | `10s` |
| Tx Gossip Throttling Limit | int | Throttling limit | `2` |
| Tx Gossip Poll Size | int | Poll size | `1` |

## Validation Notes

- Cannot enable `populate-missing-tries` while pruning or offline pruning is enabled
- Cannot run offline pruning while pruning is disabled  
- Commit interval must be non-zero when pruning is enabled
- `push-gossip-percent-stake` must be in range `[0, 1]`
- Some settings may require node restart to take effect
