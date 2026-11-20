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

## Shared Options

The C-Chain also accepts the options shared with L1s, described in the [shared configuration](../vmconfig/config.md):

- [Block Building](../vmconfig/config.md#block-building)
- [State and Trie](../vmconfig/config.md#state-and-trie)
- [Transaction Pool](../vmconfig/config.md#transaction-pool)
- [APIs](../vmconfig/config.md#apis)
- [State Sync](../vmconfig/config.md#state-sync)
- [Warp](../vmconfig/config.md#warp)

## Block Building

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| `min-price-target` | integer | The target minimum gas price, in wei (aAVAX), that this node will attempt to use when creating blocks. | Parent block's target |
| `gas-target` | integer | The target gas per second that this node will attempt to use when creating blocks. | Parent block's target |
