<!-- markdownlint-disable MD041 MD033 -->
> **Note**: These are the configuration options available for L1s. To set these values, you need to create a configuration file at `{chain-config-dir}/{blockchainID}/config.json`. This file does not exist by default.
>
> For example if `chain-config-dir` has the default value which is `$HOME/.avalanchego/configs/chains`, then `config.json` should be placed at `$HOME/.avalanchego/configs/chains/{blockchainID}/config.json`.
>
> For the AvalancheGo node configuration options, see the AvalancheGo Configuration page.

This document describes the configuration options available for L1s. Default values for each option are specified below.

Default values are overridden only if specified in the given config file. It is recommended to only provide values which are different from the default, as that makes the config more resilient to future default changes. Otherwise, if defaults change, your node will remain with the old values, which might adversely affect your node operation.

## Example Configuration

```json
{
  "pruning-enabled": true,
  "commit-interval": 4096,
  "local-txs-enabled": false,
  "tx-pool-account-slots": 16,
  "tx-pool-global-slots": 5120
}
```

## Configuration Format

Configuration is provided as a JSON object. All fields are optional unless otherwise specified.

Unrecognized options — a typo, or an option of Subnet-EVM that no longer exists — are ignored, and the node logs a warning naming them. The one exception is [`eth-apis`](#deprecated-eth-apis), which is deprecated but still honoured.

## Shared Options

L1s accept the options shared with the C-Chain, described in the [shared configuration](../vmconfig/config.md):

- [Block Building](../vmconfig/config.md#block-building)
- [State and Trie](../vmconfig/config.md#state-and-trie)
- [Transaction Pool](../vmconfig/config.md#transaction-pool)
- [APIs](../vmconfig/config.md#apis)

### Deprecated: `eth-apis`

`eth-apis`, Subnet-EVM's API allowlist, is deprecated and **WILL BE REMOVED in
the next release**; migrate to `apis`. Until then, the node maps each
`eth-apis` name onto the `apis` values that serve the same methods and logs the
resulting `apis` value to copy into the config. Names whose methods no longer
exist (e.g. `admin`, `internal-personal`) are ignored with a warning, and an
unrecognised name remains a fatal configuration error. If a config sets both
options, `apis` wins and `eth-apis` is ignored.
