<!-- markdownlint-disable MD041 MD033 -->
> **Note**: These are the configuration options available for the SAE L1 VM. To set these values, create a configuration file at `{chain-config-dir}/{blockchainID}/config.json`. This file does not exist by default.
>
> For example if `chain-config-dir` has the default value which is `$HOME/.avalanchego/configs/chains`, then `config.json` should be placed at `$HOME/.avalanchego/configs/chains/{blockchainID}/config.json`.
>
> For the AvalancheGo node configuration options, see the AvalancheGo Configuration page.

This document describes the configuration options available for the SAE L1 VM. Default values for each option are specified below.

Default values are overridden only if specified in the given config file. It is recommended to only provide values which are different from the default, as that makes the config more resilient to future default changes. Otherwise, if defaults change, your node will remain with the old values, which might adversely affect your node operation.

This VM accepts every [C-Chain option](../cchain/config.md) plus the L1-specific options below. `min-price-target` and `state-sync-enabled` are accepted but not yet supported, so they have no effect. Unrecognized options, such as a typo or a legacy Subnet-EVM option this VM doesn't support, are ignored, and the node logs a warning naming them. The exceptions are the [deprecated options](#deprecated-options), which are still honoured.

## Example Configuration

```json
{
  "pruning-enabled": true,
  "commit-interval": 4096,
  "local-txs-enabled": false,
  "tx-pool-account-slots": 16,
  "tx-pool-global-slots": 5120,
  "fee-recipient": "0x0100000000000000000000000000000000000000",
  "warp-off-chain-messages": []
}
```

## Block Building

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `min-delay-target` | integer | The minimum delay between blocks (in milliseconds) that this node will attempt to use when creating blocks. | Parent block's target |
| `gas-target` | integer | The target gas per second that this node will attempt to use when creating blocks. Ignored while the `gaspricemanager` precompile pins a validator-independent target. | Parent block's target |
| `fee-recipient` | string | `0x`-prefixed address paid the fees of blocks built by this node, if the chain allows custom fee recipients (genesis `allowFeeRecipients` or the `rewardmanager` precompile's `allowFeeRecipients()`). The default burns fees, and the node logs a warning at startup if the chain allows claiming them. | `"0x0100000000000000000000000000000000000000"` |

## State and Trie

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `pruning-enabled` | bool | Enable state pruning to save disk space. If disabled, the node runs in archival mode and retains all historical state. When enabled, trie roots are only persisted every `commit-interval` blocks. | `true` |
| `commit-interval` | uint64 | For HashDB, this sets the number of blocks between state-trie writes to disk. For Firewood, it sets the maximum number of revisions that may remain unpersisted at once. Omit this option to use the default; Mainnet and Fuji MUST use the default with HashDB. The node does not accept an explicit 0. | `4096` |
| `trie-clean-cache` | int | Size of the trie clean cache in MB. | `512` |
| `snapshot-cache` | int | Size of the snapshot disk layer clean cache in MB. | `256` |
| `allow-missing-tries` | bool | Suppress warnings about an incomplete trie index. | `false` |
| `state-scheme` | string | EXPERIMENTAL: specifies the database scheme used to store state data; either `hash` or `firewood`. | `hash` |
| `state-history` | uint64 | Number of recently executed states to guarantee available in memory. The minimum possible value is the default. | `128` |

## Transaction Pool

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `local-txs-enabled` | bool | Enable treatment of transactions from local accounts as local. Local transactions receive preferential admission and pricing in the mempool. The on-disk transaction journal stays disabled either way, matching the legacy plugin. | `false` |
| `tx-pool-account-slots` | uint64 | Maximum number of executable transaction slots per account. | `16` |
| `tx-pool-global-slots` | uint64 | Maximum number of executable transaction slots for all accounts. | `5120` |

## APIs

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `apis` | array of strings | The JSON-RPC APIs this node serves, see the C-Chain's [Available APIs](../cchain/config.md#available-apis). This VM also serves `validator`, the `validator_*` methods. Methods of an API that is not listed are not served. An unrecognised name is a fatal configuration error. | `validator` and every API enabled by default on the C-Chain |
| `allow-unprotected-txs` | bool | Allow unprotected transactions (without EIP-155 replay protection). | `false` |
| `api-max-blocks-per-request` | uint64 | Maximum number of blocks per `eth_getLogs` request (`0` = no limit). | `0` |
| `api-max-duration` | duration | Maximum duration of an `eth_call` or `eth_callDetailed`; non-positive values disable the limit. Accepts a [Go duration string](https://pkg.go.dev/time#ParseDuration), such as `"30s"`. | `"0s"` |
| `batch-request-limit` | uint64 | Maximum requests in a JSON-RPC batch (`0` = no limit). | `1000` |
| `api-resolve-pending-to-last-executed` | bool | Resolve pending-state RPC requests against the last executed block. | `true` |

## Warp

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `warp-off-chain-messages` | array of strings | Hex-encoded off-chain Warp messages the node should be willing to sign. These messages do not need to correspond to any on-chain event. | empty array |

## Database

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `use-standalone-database` | bool | Store chain data in a database opened by this VM, instead of the node's database. Ignored if the node's database already holds the chain. | `true` |
| `database-type` | string | Type of the standalone database, one of `leveldb`, `memdb` or `pebbledb`. | `pebbledb` |
| `database-path` | string | Parent directory of the standalone database, which is stored in `{database-path}/{database-type}`. | `{chain-data-dir}/db` |
| `database-config` | object | JSON config of the standalone database. | unset |

## Deprecated Options

These legacy Subnet-EVM options are deprecated and **WILL BE REMOVED in the next release**. Until then, the node converts each one to its replacement and logs the value to copy into the config. If a config sets both a deprecated option and its replacement, the replacement wins.

| Option | Replacement | Notes |
|--------|-------------|-------|
| `feeRecipient` | `fee-recipient` | Accepted with or without the `0x` prefix. An invalid address burns fees, with a warning. |
| `eth-apis` | `apis` | Each name maps onto the `apis` values that serve the same methods. Names whose methods no longer exist, such as `admin` and `internal-personal`, are ignored with a warning. An unrecognised name is a fatal configuration error. |
| `validators-api-enabled` | `apis` | Adds or removes `validator`. |
| `warp-api-enabled` | none | The `warp_*` methods are no longer served. Enabling it logs a warning. |
| `admin-api-enabled` | none | The `admin_*` methods are no longer served. Enabling it logs a warning. |
| `database-config` as a base64 string | `database-config` as a JSON object | The string is decoded. |
| `database-config-file` | `database-config` | The file's JSON is read at startup. |
