<!-- markdownlint-disable MD041 MD033 -->
> **Note**: These are the configuration options available for the SAE Subnet-EVM VM. To set these values, create a configuration file at `{chain-config-dir}/{blockchainID}/config.json`. This file does not exist by default.
>
> For example if `chain-config-dir` has the default value which is `$HOME/.avalanchego/configs/chains`, then `config.json` should be placed at `$HOME/.avalanchego/configs/chains/{blockchainID}/config.json`.
>
> For the AvalancheGo node configuration options, see the AvalancheGo Configuration page.

This document describes the configuration options available for the SAE Subnet-EVM VM. Default values for each option are specified below.

Default values are overridden only if specified in the given config file. It is recommended to only provide values which are different from the default, as that makes the config more resilient to future default changes. Otherwise, if defaults change, your node will remain with the old values, which might adversely affect your node operation.

Field names and JSON tags match the legacy Subnet-EVM plugin's config schema for the subset this VM supports, so existing operator config blobs keep working for those fields. **Unknown fields are rejected**: legacy-only knobs (e.g. gossip tuning, `state-sync-enabled`, `use-standalone-database`, `offline-pruning-*`, `admin-api-enabled`) surface as a startup error rather than silently no-opping. Remove them from the config file before pointing it at this VM.

## Example Configuration

```json
{
  "pruning-enabled": true,
  "commit-interval": 4096,
  "local-txs-enabled": false,
  "tx-pool-account-slots": 16,
  "tx-pool-global-slots": 5120,
  "feeRecipient": "",
  "warp-off-chain-messages": []
}
```

## Block Building

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `min-delay-target` | integer | The minimum delay between blocks (in milliseconds) that this node will attempt to use when creating blocks. | Parent block's target |
| `gas-target` | integer | The target gas per second that this node will attempt to use when creating blocks. Ignored while the `gaspricemanager` precompile pins a validator-independent target. | Parent block's target |
| `feeRecipient` | string | Hex address to stamp into `header.Coinbase` when the chain allows custom fee recipients (genesis `allowFeeRecipients` or the `rewardmanager` precompile's `allowFeeRecipients()`). Empty means fees are burned; the node logs a warning at startup if the chain would have allowed claiming them. | `""` |

## State and Trie

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `pruning-enabled` | bool | Enable state pruning to save disk space. If disabled, the node runs in archival mode and retains all historical state. When enabled, trie roots are only persisted every `commit-interval` blocks. | `true` |
| `commit-interval` | uint64 | Interval at which to persist the state trie (blocks). A value of `0` is rejected. | `4096` |

## Transaction Pool

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `local-txs-enabled` | bool | Enable treatment of transactions from local accounts as local. Local transactions receive preferential admission and pricing in the mempool. The on-disk transaction journal stays disabled either way, matching the legacy plugin. | `false` |
| `tx-pool-price-limit` | uint64 | Minimum gas price (in wei) to enforce for acceptance into the pool. | `1` |
| `tx-pool-price-bump` | uint64 | Minimum price bump percentage to replace an already existing transaction (nonce). | `10` |
| `tx-pool-account-slots` | uint64 | Maximum number of executable transaction slots per account. | `16` |
| `tx-pool-global-slots` | uint64 | Maximum number of executable transaction slots for all accounts. | `5120` |
| `tx-pool-account-queue` | uint64 | Maximum number of non-executable transaction slots per account. | `64` |
| `tx-pool-global-queue` | uint64 | Maximum number of non-executable transaction slots for all accounts. | `1024` |
| `tx-pool-lifetime` | duration | Maximum time non-executable transactions remain queued, as a duration string (`"10m"`) or nanoseconds. | `"10m"` |

## APIs

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `rpc-gas-cap` | uint64 | Maximum gas for `eth_call`/`eth_estimateGas`-style RPC execution. | `50000000` |
| `rpc-tx-fee-cap` | float64 | Cap on transaction fees (in the native token) that can be sent via RPC APIs (`0` = no cap). | `100` |
| `api-max-duration` | duration | Maximum duration of an `eth_call` or `eth_callDetailed`; non-positive values disable the limit. Accepts a duration string or nanoseconds. | `"0s"` |
| `batch-request-limit` | uint64 | Maximum requests in a JSON-RPC batch (`0` = no limit). | `1000` |
| `api-resolve-pending-to-last-executed` | bool | Resolve pending-state RPC requests against the last executed block. | `true` |

## Warp

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `warp-off-chain-messages` | array of strings | Hex-encoded off-chain Warp messages the node should be willing to sign. These messages do not need to correspond to any on-chain event. | empty array |

## Logging

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `log-level` | string | Log level for both the libevm-internal logger and the chain's AvalancheGo logger: `trace`, `debug`, `info`, `warn`, `error`, or `crit`. Empty leaves the loggers as the node configured them. | `""` |
