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
  "disable-tracing-api": false,
  "warp-off-chain-messages": []
}
```

## Configuration Format

Configuration is provided as a JSON object. All fields are optional unless otherwise specified.

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
| `state-scheme` | string | EXPERIMENTAL: specifies the database scheme used to store state data; one of `hash`, `firewood`, or `path`. | `hash` |

## Transaction Pool

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `local-txs-enabled` | bool | Enable treatment of transactions from local accounts as local. Local transactions receive preferential admission and pricing in the mempool. | `false` |
| `tx-pool-account-slots` | uint64 | Maximum number of executable transaction slots per account. | `16` |
| `tx-pool-global-slots` | uint64 | Maximum number of executable transaction slots for all accounts. | `5120` |

## APIs

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `disable-tracing-api` | bool | Disable the transaction tracing API. When `true`, the following RPC calls are removed from the `debug_*` namespace: <br/> - `debug_traceChain` <br/> - `debug_traceBlockByNumber` <br/> - `debug_traceBlockByHash` <br/> - `debug_traceBlock` <br/> - `debug_traceBadBlock` <br/> - `debug_intermediateRoots` <br/> - `debug_traceTransaction` <br/> - `debug_traceCall` | `false` |
| `api-max-blocks-per-request` | int64 | Maximum number of blocks per `eth_getLogs` request (`0` = no limit). | `0` |
| `allow-unprotected-txs` | bool | Allow unprotected transactions (without EIP-155 replay protection). | `false` |
| `batch-request-limit` | uint64 | Maximum number of requests that can be batched in an RPC call (`0` = no limit). | `1000` |

## State Sync

> **Note:** If state sync is enabled, the peer will download chain state from peers up to a recent block near tip, then proceed with normal bootstrapping. If you need historical data, state sync isn't the right option; however, it is sufficient if you are just running a validator.

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `state-sync-enabled` | bool | Enable state sync. | `false` |
| `state-sync-ids` | string | Comma-separated list of state sync IDs. If not specified (or empty), peers are selected at random. | `""` |

## Warp

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `warp-off-chain-messages` | array of strings | Hex-encoded off-chain Warp messages the node should be willing to sign. These messages do not need to correspond to any on-chain event. | empty array |
