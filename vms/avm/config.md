---
tags: [Nodes, AvalancheGo]
description: Reference for all available X-chain config options and flags.
pagination_label: X-Chain Configs
sidebar_position: 2
---

# X-Chain

In order to specify a config for the X-Chain, a JSON config file should be
placed at `{chain-config-dir}/X/config.json`.

For example if `chain-config-dir` has the default value which is
`$HOME/.avalanchego/configs/chains`, then `config.json` can be placed at
`$HOME/.avalanchego/configs/chains/X/config.json`.

This allows you to specify a config to be passed into the X-Chain. The default
values for this config are:

```json
{
  "index-transactions": false,
  "index-allow-incomplete": false,
  "checksums-enabled": false
}
```

Default values are overridden only if explicitly specified in the config.

The parameters are as follows:

## Transaction Indexing

### `index-transactions`

_Boolean_

Enables AVM transaction indexing if set to `true`.
When set to `true`, AVM transactions are indexed against the `address` and
`assetID` involved. This data is available via `avm.getAddressTxs`
[API](/reference/avalanchego/x-chain/api.md#avmgetaddresstxs).

:::note
If `index-transactions` is set to true, it must always be set to true
for the node's lifetime. If set to `false` after having been set to `true`, the
node will refuse to start unless `index-allow-incomplete` is also set to `true`
(see below).
:::

### `index-allow-incomplete`

_Boolean_

Allows incomplete indices. This config value is ignored if there is no X-Chain indexed data in the DB and
`index-transactions` is set to `false`.

### `checksums-enabled`

_Boolean_

Enables checksums if set to `true`.
