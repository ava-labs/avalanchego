> **Note**: These are the configuration options available in the coreth codebase. To set these values, you need to create a configuration file at `{chain-config-dir}/C/config.json`. This file does not exist by default.
>
> For example if `chain-config-dir` has the default value which is `$HOME/.avalanchego/configs/chains`, then `config.json` should be placed at `$HOME/.avalanchego/configs/chains/C/config.json`.
>
> For the AvalancheGo node configuration options, see the AvalancheGo Configuration page.

This document describes all configuration options available for coreth.

The C-Chain config is printed out in the log when a node starts. Default values for each config flag are specified below.

Default values are overridden only if specified in the given config file. It is recommended to only provide values which are different from the default, as that makes the config more resilient to future default changes. Otherwise, if defaults change, your node will remain with the old values, which might adversely affect your node operation.

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
| `eth` | bool | Adds the `eth_coinbase` and `eth_etherbase` RPC calls to the `eth_*` namespace. | `true` | 
| `eth-filter` | bool |  Enables the public filter API for the `eth_*` namespace and adds the following RPC calls (see [here](https://eth.wiki/json-rpc/API) for complete documentation): <br/> - `eth_newPendingTransactionFilter` <br/> - `eth_newPendingTransactions` <br/> - `eth_newAcceptedTransactions` <br/> - `eth_newBlockFilter` <br/> - `eth_newHeads` <br/> - `eth_logs` <br/> - `eth_newFilter` <br/> - `eth_getLogs` <br/> - `eth_uninstallFilter` <br/> - `eth_getFilterLogs` <br/> - `eth_getFilterChanges` <br/> |   `true` | 
| `admin` | bool | Adds the `admin_importChain` and `admin_exportChain` RPC calls to the `admin_*` namespace | `false` | 
| `debug` | bool | Adds the following RPC calls to the `debug_*` namespace. <br/> - `debug_dumpBlock` <br/> - `debug_accountRange` <br/> - `debug_preimage` <br/> - `debug_getBadBlocks` <br/> - `debug_storageRangeAt` <br/> - `debug_getModifiedAccountsByNumber` <br/> - `debug_getModifiedAccountsByHash` <br/> - `debug_getAccessibleState` <br/> The following RPC calls are disabled for any nodes with `state-scheme = firewood`: <br/> - `debug_storageRangeAt` <br/> - `debug_getModifiedAccountsByNumber` <br/> - `debug_getModifiedAccountsByHash` <br/> | `false`.
| `net` | bool | Adds the following RPC calls to the `net_*` namespace. <br/> - `net_listening` <br/> - `net_peerCount` <br/> - `net_version` <br/> Note: Coreth is a virtual machine and does not have direct access to the networking layer, so `net_listening` always returns true and `net_peerCount` always returns 0. For accurate metrics on the network layer, users should use the AvalancheGo APIs. | `true` |
| `debug-tracer` | bool | Adds the following RPC calls to the `debug_*` namespace. <br/> - `debug_traceChain` <br/> - `debug_traceBlockByNumber` <br/> - `debug_traceBlockByHash` <br/> - `debug_traceBlock` <br/> - `debug_traceBadBlock` <br/> - `debug_intermediateRoots` <br/> - `debug_traceTransaction` <br/> - `debug_traceCall` | `false` |
| `web3` | bool | Adds the `web3_clientVersion` and `web3_sha3` RPC calls to the `web3_*` namespace | `true` |
| `internal-eth` | bool | Adds the following RPC calls to the `eth_*` namespace. <br/> - `eth_gasPrice` <br/> - `eth_baseFee` <br/> - `eth_maxPriorityFeePerGas` <br/> - `eth_feeHistory` | `true` |
| `internal-blockchain` | bool | Adds the following RPC calls to the `eth_*` namespace. <br/> - `eth_chainId` <br/> - `eth_blockNumber` <br/> - `eth_getBalance` <br/> - `eth_getProof` <br/> - `eth_getHeaderByNumber` <br/> - `eth_getHeaderByHash` <br/> - `eth_getBlockByNumber` <br/> - `eth_getBlockByHash` <br/> - `eth_getUncleBlockByNumberAndIndex` <br/> - `eth_getUncleBlockByBlockHashAndIndex` <br/> - `eth_getUncleCountByBlockNumber` <br/> - `eth_getUncleCountByBlockHash` <br/> - `eth_getCode` <br/> - `eth_getStorageAt` <br/> - `eth_call` <br/> - `eth_estimateGas` <br/> - `eth_createAccessList` <br/> `eth_getProof` is disabled for any node with `state-scheme = firewood` | `true` |
| `internal-transaction` | bool | Adds the following RPC calls to the `eth_*` namespace. <br/> - `eth_getBlockTransactionCountByNumber` <br/> - `eth_getBlockTransactionCountByHash` <br/> - `eth_getTransactionByBlockNumberAndIndex` <br/> - `eth_getTransactionByBlockHashAndIndex` <br/> - `eth_getRawTransactionByBlockNumberAndIndex` <br/> - `eth_getRawTransactionByBlockHashAndIndex` <br/> - `eth_getTransactionCount` <br/> - `eth_getTransactionByHash` <br/> - `eth_getRawTransactionByHash` <br/> - `eth_getTransactionReceipt` <br/> - `eth_sendTransaction` <br/> - `eth_fillTransaction` <br/> - `eth_sendRawTransaction` <br/> - `eth_sign` <br/> - `eth_signTransaction` <br/> - `eth_pendingTransactions` <br/> - `eth_resend` | `true` |
| `internal-tx-pool` | bool | Adds the following RPC calls to the `txpool_*` namespace. <br/> - `txpool_content` <br/> - `txpool_contentFrom` <br/> - `txpool_status` <br/> - `txpool_inspect` | `false` |
| `internal-debug` | bool | Adds the following RPC calls to the `debug_*` namespace. <br/> - `debug_getHeaderRlp` <br/> - `debug_getBlockRlp` <br/> - `debug_printBlock` <br/> - `debug_chaindbProperty` <br/> - `debug_chaindbCompact` | `false` |
| `debug-handler` | bool | Adds the following RPC calls to the `debug_*` namespace. <br/> - `debug_verbosity` <br/> - `debug_vmodule` <br/> - `debug_backtraceAt` <br/> - `debug_memStats` <br/> - `debug_gcStats` <br/> - `debug_blockProfile` <br/> - `debug_setBlockProfileRate` <br/> - `debug_writeBlockProfile` <br/> - `debug_mutexProfile` <br/> - `debug_setMutexProfileFraction` <br/> - `debug_writeMutexProfile` <br/> - `debug_writeMemProfile` <br/> - `debug_stacks` <br/> - `debug_freeOSMemory` <br/> - `debug_setGCPercent` | `false` |
| `internal-account` | bool | Adds the `eth_accounts` RPC call to the `eth_*` namespace  | `true` |
| `internal-personal` | bool | Adds the following RPC calls to the `personal_*` namespace. <br/> - `personal_listAccounts` <br/> - `personal_listWallets` <br/> - `personal_openWallet` <br/> - `personal_deriveAccount` <br/> - `personal_newAccount` <br/> - `personal_importRawKey` <br/> - `personal_unlockAccount` <br/> - `personal_lockAccount` <br/> - `personal_sendTransaction` <br/> - `personal_signTransaction` <br/> - `personal_sign` <br/> - `personal_ecRecover` <br/> - `personal_signAndSendTransaction` <br/> - `personal_initializeWallet` <br/> - `personal_unpair` | `false` |

### Enabling Avalanche Specific APIs

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `admin-api-enabled` | bool | Enables the Admin API |  `false` | 
| `admin-api-dir` | string | Specifies the directory for the Admin API to use to store CPU/Mem/Lock Profiles | `""` | 
| `warp-api-enabled` | bool | Enable the Warp API for cross-chain messaging | `false` |

### API Limits and Security

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `rpc-gas-cap` | uint64 | Maximum gas limit for RPC calls | `50,000,000` |
| `rpc-tx-fee-cap` | float64 | Maximum transaction fee cap in AVAX | `100` |
| `api-max-duration` | duration | Maximum duration for API calls (0 = no limit) | `0` |
| `api-max-blocks-per-request` | int64 | Maximum number of blocks per getLogs request (0 = no limit) | `0` |
| `http-body-limit` | uint64 | Maximum size of HTTP request bodies (0 = no limit) | `0` |
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

> **⚠️ WARNING: `allow-unfinalized-queries` should likely be set to `false` in production.** Enabling this flag can result in a confusing/unreliable user experience. Enabling this flag should only be done when users are expected to have knowledge of how Snow* consensus finalizes blocks.
> Unlike chains with reorgs and forks that require block confirmations, Avalanche **does not** increase the confidence that a block will be finalized based on the depth of the chain. Waiting for additional blocks **does not** confirm finalization. Enabling this flag removes the guarantee that the node only exposes finalized blocks; requiring users to guess if a block is finalized.

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `preimages-enabled` | bool | Enable preimage recording | `false` |
| `allow-unfinalized-queries` | bool | Allow queries for unfinalized blocks | `false` |
| `allow-unprotected-txs` | bool | Allow unprotected transactions (without EIP-155) | `false` |
| `allow-unprotected-tx-hashes` | \[\]TxHash | Specifies an array of transaction hashes that should be allowed to bypass replay protection. This flag is intended for node operators that want to explicitly allow specific transactions to be issued through their API. | an empty list |
| `local-txs-enabled` | bool | Enable treatment of transactions from local accounts as local | `false` |

### Snapshots

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `snapshot-wait` | bool | Wait for snapshot generation on startup | `false` |
| `snapshot-verification-enabled` | bool | Enable snapshot verification | `false` |

## Pruning and State Management

 > **Note**: If a node is ever run with `pruning-enabled` as `false` (archival mode), setting `pruning-enabled` to `true` will result in a warning and the node will shut down. This is to protect against unintentional misconfigurations of an archival node. To override this and switch to pruning mode, in addition to `pruning-enabled: true`, `allow-missing-tries` should be set to `true` as well. 

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

> **Note**: If offline pruning is enabled it will  run on startup and block until it completes (approximately one hour on Mainnet). This will reduce the size of the database by deleting old trie nodes. **While performing offline pruning, your node will not be able to process blocks and will be considered offline.** While ongoing, the pruning process consumes a small amount of additional disk space (for deletion markers and the bloom filter). For more information see [here.](https://build.avax.network/docs/nodes/maintain/reduce-disk-usage#disk-space-considerations). Since offline pruning deletes old state data, this should not be run on nodes that need to support archival API requests. This is meant to be run manually, so after running with this flag once, it must be toggled back to false before running the node again. Therefore, you should run with this flag set to true and then set it to false on the subsequent run.

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
| `tx-pool-price-limit` | uint64 | Minimum gas price for transaction acceptance | `1` |
| `tx-pool-price-bump` | uint64 | Minimum price bump percentage for transaction replacement | `10%` |
| `tx-pool-account-slots` | uint64 | Maximum number of executable transaction slots per account | `16`  |
| `tx-pool-global-slots` | uint64 | Maximum number of executable transaction slots for all accounts | `5120` |
| `tx-pool-account-queue` | uint64 | Maximum number of non-executable transaction slots per account | `64` |
| `tx-pool-global-queue` | uint64 | Maximum number of non-executable transaction slots for all accounts | `1024` |
| `tx-pool-lifetime` | duration | Maximum duration in nanoseconds a non-executable transaction will be allowed in the poll | `600000000000` (10 minutes) |
| `price-options-slow-fee-percentage` | integer | Percentage to apply for slow fee estimation | `95` |
| `price-options-fast-fee-percentage` | integer | Percentage to apply for fast fee estimation | `105` |
| `price-options-max-tip` | integer | Maximum tip in wei for fee estimation | `20000000000` (20 Gwei) |

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
| `continuous-profiler-dir` | string | Directory for continuous profiler output (empty = disabled) | `""` |
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
| `keystore-directory` | string | Directory for keystore files (absolute or relative path); if empty, uses a temporary directory at `coreth-keystore` |  `""` |
| `keystore-external-signer` | string | Specifies an external URI for a clef-type signer |  `""` |
| `keystore-insecure-unlock-allowed` | bool | Allow insecure account unlocking | `false` |

## Network and Sync

### Network

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `max-outbound-active-requests` | int64 | Maximum number of outbound active requests for VM2VM network | `16` |

### State Sync

> **Note:** If state-sync is enabled, the peer will download chain state from peers up to a recent block near tip, then proceed with normal bootstrapping. Please note that if you need historical data, state sync isn't the right option. However, it is sufficient if you are just running a validator. 

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `state-sync-enabled` | bool | Enable state sync | `false` |
| `state-sync-skip-resume` | bool | Force state sync to use highest available summary block | `false` |
| `state-sync-ids` | string | Comma-separated list of state sync IDs; If not specified (or empty), peers are selected at random.| `""`|
| `state-sync-commit-interval` | uint64 | Commit interval for state sync (blocks) | `16384` |
| `state-sync-min-blocks` | uint64 | Minimum blocks ahead required for state sync | `300000` |
| `state-sync-request-size` | uint16 | Number of key/values to request per state sync request | `1024` |

## Database Configuration

> **WARNING**: `firewood` scheme is untested in production. To use `firewood`, you must also set the following config options:
>
> - `pruning-enabled: true` (enabled by default)
> - `state-sync-enabled: false`
> - `snapshot-cache: 0`

Failing to set these options will result in errors on VM initialization. Additionally, not all APIs are available - see these portions of the config documentation for more details.

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `inspect-database` | bool | Inspect database on startup | `false` |
| `state-scheme` | string |  EXPERIMENTAL: specifies the database scheme to store state data; can be one of `hash` or `firewood` | `hash` | 

## Transaction Indexing

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `transaction-history` | uint64 | Maximum number of blocks from head whose transaction indices are reserved (0 = no limit) | `0` |
| `skip-tx-indexing` | bool | Skip indexing transactions entirely | `false` |

## Warp Configuration

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `warp-off-chain-messages` | array | Off-chain messages the node should be willing to sign | empty array |
| `prune-warp-db-enabled` | bool | Clear warp database on startup | `false` |

## Miscellaneous

> **Note:**  If `skip-upgrade-check` is set to `true`, the chain will skip verifying that all expected network upgrades have taken place before the last accepted block on startup. This allows node operators to recover if their node has accepted blocks after a network upgrade with a version of the code prior to the upgrade.

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `acceptor-queue-limit` | integer | Specifies the maximum number of blocks to queue during block acceptance before blocking on Accept. | `64` |
| `gas-target` | integer | The target gas per second that this node will attempt to use when creating blocks | Parent block's target | 
| `skip-upgrade-check` | bool | Skip checking that upgrades occur before last accepted block ⚠️ **Warning**: Only use when you understand the implications | `false` |
