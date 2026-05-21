In order to specify a config for the X-Chain, a JSON config file should be
placed at `{chain-config-dir}/X/config.json`.

For example if `chain-config-dir` has the default value which is
`$HOME/.avalanchego/configs/chains`, then `config.json` can be placed at
`$HOME/.avalanchego/configs/chains/X/config.json`.

This allows you to specify a config to be passed into the X-Chain. The default
values for this config are:

```json
{
  "checksums-enabled": false
}
```

Default values are overridden only if explicitly specified in the config.

The parameters are as follows:

### `checksums-enabled`

_Boolean_

Enables checksums if set to `true`.
