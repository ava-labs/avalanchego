<!-- markdownlint-disable MD041 MD033 -->
> **Note**: These are the configuration options available for L1 VMs. To set these values, create a configuration file at `{chain-config-dir}/{blockchainID}/config.json`. This file does not exist by default.
>
> For example if `chain-config-dir` has the default value which is `$HOME/.avalanchego/configs/chains`, then `config.json` should be placed at `$HOME/.avalanchego/configs/chains/{blockchainID}/config.json`.
>
> For the AvalancheGo node configuration options, see the AvalancheGo Configuration page.

Only set options that differ from their defaults, so that the node picks up future default changes.

## Example Configuration

```json
{
  "fee-recipient": "0x0123456789abcdef0123456789abcdef01234567",
  "pruning-enabled": false
}
```

## Configuration Format

This VM accepts every [C-Chain option](../cchain/config.md). This document covers only the options that are new or behave differently.

The node ignores unrecognized options and logs a warning naming them. [Deprecated options](#deprecated-options) are converted to their replacements.

## Differences from the C-Chain

- `gas-target` is ignored while the `gaspricemanager` precompile sets the target.
- `apis` also accepts `validator`, which serves `validator_uptime` and is enabled by default.
- `eth_subscribe` is served at `/ext/bc/{blockchainID}/ws`.

## Block Building

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `fee-recipient` | string | `0x`-prefixed address paid the fees of blocks built by this node, if the chain allows custom fee recipients. The node warns for each block that burns fees it could claim. | `"0x0100000000000000000000000000000000000000"`, which burns fees |

## Database

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `use-standalone-database` | bool | Store chain data in a database opened by this VM, instead of the node's database. Ignored if the node's database already holds the chain. | `true` |
| `database-type` | string | Type of the standalone database, one of `leveldb`, `memdb` or `pebbledb`. | `pebbledb` |
| `database-path` | string | Parent directory of the standalone database, which is stored in `{database-path}/{database-type}`. | `{chain-data-dir}/db` |
| `database-config` | object | JSON config of the standalone database. | unset |

## Deprecated Options

These options **WILL BE REMOVED in the next release**. Until then, the node converts each one to its replacement and logs a warning. If both are set, the replacement wins.

| Option | Replacement | Notes |
|--------|-------------|-------|
| `feeRecipient` | `fee-recipient` | The `0x` prefix is optional. An invalid address burns fees. |
| `eth-apis` | `apis` | Each name maps to the `apis` that serve its methods. Names whose methods no longer exist, such as `admin`, are ignored. An unrecognised name is a fatal configuration error. |
| `validators-api-enabled` | `apis` | Adds or removes `validator`. |
| `warp-api-enabled` | none | The `warp_*` methods no longer exist. |
| `admin-api-enabled` | none | The `admin_*` methods no longer exist. |
| `database-config` as a base64 string | `database-config` as a JSON object | |
| `database-config-file` | `database-config` | |
