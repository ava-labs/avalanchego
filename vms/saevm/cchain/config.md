<!-- markdownlint-disable MD041 MD033 -->
> **Note**: These are the configuration options available for the C-Chain. To set these values, you need to create a configuration file at `{chain-config-dir}/C/config.json`. This file does not exist by default.
>
> For example if `chain-config-dir` has the default value which is `$HOME/.avalanchego/configs/chains`, then `config.json` should be placed at `$HOME/.avalanchego/configs/chains/C/config.json`.
>
> For the AvalancheGo node configuration options, see the AvalancheGo Configuration page.

This document describes the configuration options available for the C-Chain. Default values for each option are specified below.

Default values are overridden only if specified in the given config file. It is recommended to only provide values which are different from the default, as that makes the config more resilient to future default changes. Otherwise, if defaults change, your node will remain with the old values, which might adversely affect your node operation.

## Example Configuration

```json
{
  "pruning-enabled": true,
  "commit-interval": 4096,
  "local-txs-enabled": false,
  "tx-pool-account-slots": 16,
  "tx-pool-global-slots": 5120,
  "warp-off-chain-messages": []
}
```

## Configuration Format

Configuration is provided as a JSON object. All fields are optional unless otherwise specified.

Unrecognized options — a typo, or an option of the pre-SAE C-Chain that no longer exists — are ignored, and the node logs a warning naming them. The one exception is [`eth-apis`](#deprecated-eth-apis), which is deprecated but still honoured.

## Block Building

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `min-price-target` | integer | The target minimum gas price, in wei (aAVAX), that this node will attempt to use when creating blocks. | Parent block's target |
| `gas-target` | integer | The target gas per second that this node will attempt to use when creating blocks. | Parent block's target |
| `min-delay-target` | integer | The minimum delay between blocks (in milliseconds) that this node will attempt to use when creating blocks. | Parent block's target |

## State and Trie

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `pruning-enabled` | bool | Enable state pruning to save disk space. If disabled, the node runs in archival mode and retains all historical state. When enabled, trie roots are only persisted every `commit-interval` blocks. | `true` |
| `commit-interval` | uint64 | Interval at which to persist the state trie (blocks). A value of `0` uses the default. | `4096` |
| `trie-clean-cache` | int | Size of the trie clean cache in MB. | `512` |
| `snapshot-cache` | int | Size of the snapshot disk layer clean cache in MB. | `256` |
| `allow-missing-tries` | bool | Suppress warnings about an incomplete trie index. | `false` |
| `populate-missing-tries` | uint64 | Starting block for re-populating missing tries. Re-generation is disabled if null. | `null` |
| `offline-pruning-enabled` | bool | Enable offline pruning. | `false` |
| `state-scheme` | string | EXPERIMENTAL: specifies the database scheme used to store state data; either `hash` or `firewood`. | `hash` |

## Transaction Pool

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `local-txs-enabled` | bool | Enable treatment of transactions from local accounts as local. Local transactions receive preferential admission and pricing in the mempool. | `false` |
| `tx-pool-account-slots` | uint64 | Maximum number of executable transaction slots per account. | `16` |
| `tx-pool-global-slots` | uint64 | Maximum number of executable transaction slots for all accounts. | `5120` |

## APIs

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `apis` | array of strings | The JSON-RPC APIs this node serves, see [Available APIs](#available-apis). Methods of an API that is not listed are not served, and calling one returns a `the method ... does not exist/is not available` error. An unrecognised name is a fatal configuration error. | every API marked *enabled* in [Available APIs](#available-apis) |
| `api-max-blocks-per-request` | int64 | Maximum number of blocks per `eth_getLogs` request (`0` = no limit). | `0` |
| `allow-unprotected-txs` | bool | Allow unprotected transactions (without EIP-155 replay protection). | `false` |
| `batch-request-limit` | uint64 | Maximum number of requests that can be batched in an RPC call (`0` = no limit). | `1000` |
| `api-max-duration` | duration | Maximum duration of an `eth_call` (or `eth_callDetailed`) execution. Accepts a [Go duration string](https://pkg.go.dev/time#ParseDuration) (e.g. `"30s"`, `"2h45m"`); valid units are `ns`, `us`, `ms`, `s`, `m` and `h`. Non-positive values result in no limit. | `0` |
| `api-resolve-pending-to-last-executed` | bool | Requests for the "pending" block return the last-executed instead of the last-accepted to allow compatibility with EVM-ecosystem tooling that expect the pending block to have post-execution artefacts. | `true` |

### Available APIs

| Name | Default | Methods |
|------|---------|---------|
| `web3` | enabled | `web3_clientVersion`, `web3_sha3` |
| `net` | enabled | `net_listening`, `net_peerCount`, `net_version` |
| `txpool` | enabled | `txpool_content`, `txpool_contentFrom`, `txpool_inspect`, `txpool_status` |
| `gas` | enabled | `eth_feeHistory`, `eth_gasPrice`, `eth_maxPriorityFeePerGas`, `eth_syncing` |
| `chain` | enabled | Block, header, and state reads, including state execution: `eth_blockNumber`, `eth_call`, `eth_chainId`, `eth_createAccessList`, `eth_estimateGas`, `eth_getBalance`, `eth_getBlockBy{Hash,Number}`, `eth_getBlockReceipts`, `eth_getCode`, `eth_getHeaderBy{Hash,Number}`, `eth_getProof`, `eth_getStorageAt`, `eth_getUncle*` |
| `transactions` | enabled | `eth_fillTransaction`, `eth_getBlockTransactionCountBy*`, `eth_getRawTransactionBy*`, `eth_getTransactionBy*`, `eth_getTransactionCount`, `eth_getTransactionReceipt`, `eth_pendingTransactions`, `eth_resend`, `eth_sendRawTransaction`, `eth_sendTransaction`, `eth_sign`, `eth_signTransaction` |
| `subscriptions` | enabled | `eth_getFilterChanges`, `eth_getFilterLogs`, `eth_getLogs`, `eth_newBlockFilter`, `eth_newFilter`, `eth_newPendingTransactionFilter`, `eth_uninstallFilter`, and `eth_subscribe` for `logs`, `newHeads`, `newPendingTransactions`, and the Avalanche-specific `newAcceptedTransactions` |
| `avalanche` | enabled | Avalanche-specific extensions to the `eth` namespace: `eth_baseFee`, `eth_callDetailed`, `eth_getChainConfig`, `eth_suggestPriceOptions` |
| `trace` | enabled | `debug_intermediateRoots`, `debug_standardTrace{BadBlock,Block}ToFile`, `debug_traceBadBlock`, `debug_traceBlock`, `debug_traceBlockBy{Hash,Number}`, `debug_traceBlockFromFile`, `debug_traceCall`, `debug_traceChain`, `debug_traceTransaction` |
| `db` | disabled | Raw database access: `debug_chaindbCompact`, `debug_chaindbProperty`, `debug_dbAncient`, `debug_dbAncients`, `debug_dbGet`, `debug_getRawBlock`, `debug_getRawHeader`, `debug_getRawReceipts`, `debug_getRawTransaction`, `debug_printBlock`, `debug_setHead` |
| `profile` | disabled | Process introspection and profiling: `debug_blockProfile`, `debug_cpuProfile`, `debug_freeOSMemory`, `debug_gcStats`, `debug_goTrace`, `debug_memStats`, `debug_mutexProfile`, `debug_setBlockProfileRate`, `debug_setGCPercent`, `debug_setMutexProfileFraction`, `debug_stacks`, `debug_start{CPUProfile,GoTrace}`, `debug_stop{CPUProfile,GoTrace}`, `debug_verbosity`, `debug_vmodule`, `debug_write{Block,Mem,Mutex}Profile` |

Note that `eth_subscribe` is only available over the websocket endpoint
(`/ext/bc/C/ws`); the HTTP endpoint (`/ext/bc/C/rpc`) cannot deliver
notifications. Both endpoints are served by the same node and therefore serve
the same `apis`; to expose different method sets on each, run separately
configured node fleets behind each path.

### Deprecated: `eth-apis`

`eth-apis` is the API allowlist of the pre-SAE C-Chain. It is deprecated and
**WILL BE REMOVED in the next release**; migrate to `apis`. Until then,
`eth-apis` continues to work. The node maps each name to the `apis` values that
serve the same methods (see the table below). The node also logs a warning that
contains the equivalent `apis` value; copy that value to migrate. If a config
sets both options, `apis` wins and the node ignores `eth-apis` with a warning.
An unrecognised `eth-apis` name is a fatal configuration error, as it was
pre-SAE.

| `eth-apis` name | `apis` equivalent |
|-----------------|-------------------|
| `web3` | `web3` |
| `net` | `net` |
| `eth-filter` | `subscriptions` |
| `internal-eth` | `gas`, `avalanche` |
| `internal-blockchain` | `chain`, `avalanche` |
| `internal-transaction` | `transactions` |
| `internal-tx-pool` | `txpool` |
| `internal-debug` | `db` |
| `debug-tracer`, `debug-file-tracer` | `trace` |
| `debug-handler` | `profile` |
| `eth`, `admin`, `debug`, `internal-account`, `internal-personal` | none: their methods (e.g. `eth_etherbase`, the `admin` and `personal` namespaces) no longer exist and the name is ignored with a warning |

## State Sync

> **Note:** If state sync is enabled, the peer will download chain state from peers up to a recent block near tip, then proceed with normal bootstrapping. If you need historical data, state sync isn't the right option; however, it is sufficient if you are just running a validator.

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `state-sync-enabled` | bool | Enable state sync. | `true` |

## Warp

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `warp-off-chain-messages` | array of strings | Hex-encoded off-chain Warp messages the node should be willing to sign. These messages do not need to correspond to any on-chain event. | empty array |
