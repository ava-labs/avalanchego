---
tags: [X-Chain, AvalancheGo APIs]
description: This page is an overview of the Exchange Chain (X-Chain) API associated with AvalancheGo.
sidebar_label: API
pagination_label: X-Chain API
---

# X-Chain API

The [X-Chain](/learn/avalanche/avalanche-platform.md#x-chain),
Avalanche’s native platform for creating and trading assets, is an instance of the Avalanche Virtual
Machine (AVM). This API allows clients to create and trade assets on the X-Chain and other instances
of the AVM.

## Format

This API uses the `json 2.0` RPC format. For more information on making JSON RPC calls, see
[here](/reference/standards/guides/issuing-api-calls.md).

## Endpoints

`/ext/bc/X` to interact with the X-Chain.

`/ext/bc/blockchainID` to interact with other AVM instances, where `blockchainID` is the ID of a
blockchain running the AVM.

## Methods

### `avm.buildGenesis`

Given a JSON representation of this Virtual Machine’s genesis state, create the byte representation
of that state.

#### **Endpoint**

This call is made to the AVM’s static API endpoint:

`/ext/vm/avm`

Note: addresses should not include a chain prefix (that is `X-`) in calls to the static API endpoint
because these prefixes refer to a specific chain.

**Signature:**

```sh
avm.buildGenesis({
    networkID: int,
    genesisData: JSON,
    encoding: string, //optional
}) -> {
    bytes: string,
    encoding: string,
}
```

Encoding specifies the encoding format to use for arbitrary bytes, that is the genesis bytes that are
returned. Can only be `hex` when a value is provided.

`genesisData` has this form:

```json
{
"genesisData" :
    {
        "assetAlias1": {               // Each object defines an asset
            "name": "human readable name",
            "symbol":"AVAL",           // Symbol is between 0 and 4 characters
            "initialState": {
                "fixedCap" : [         // Choose the asset type.
                    {                  // Can be "fixedCap", "variableCap", "limitedTransfer", "nonFungible"
                        "amount":1000, // At genesis, address A has
                        "address":"A"  // 1000 units of asset
                    },
                    {
                        "amount":5000, // At genesis, address B has
                        "address":"B"  // 1000 units of asset
                    },
                    ...                // Can have many initial holders
                ]
            }
        },
        "assetAliasCanBeAnythingUnique": { // Asset alias can be used in place of assetID in calls
            "name": "human readable name", // names need not be unique
            "symbol": "AVAL",              // symbols need not be unique
            "initialState": {
                "variableCap" : [          // No units of the asset exist at genesis
                    {
                        "minters": [       // The signature of A or B can mint more of
                            "A",           // the asset.
                            "B"
                        ],
                        "threshold":1
                    },
                    {
                        "minters": [       // The signatures of 2 of A, B and C can mint
                            "A",           // more of the asset
                            "B",
                            "C"
                        ],
                        "threshold":2
                    },
                    ...                    // Can have many minter sets
                ]
            }
        },
        ...                                // Can list more assets
    }
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "id"     : 1,
    "method" : "avm.buildGenesis",
    "params" : {
        "networkId": 16,
        "genesisData": {
            "asset1": {
                "name": "myFixedCapAsset",
                "symbol":"MFCA",
                "initialState": {
                    "fixedCap" : [
                        {
                            "amount":100000,
                            "address": "avax13ery2kvdrkd2nkquvs892gl8hg7mq4a6ufnrn6"
                        },
                        {
                            "amount":100000,
                            "address": "avax1rvks3vpe4cm9yc0rrk8d5855nd6yxxutfc2h2r"
                        },
                        {
                            "amount":50000,
                            "address": "avax1ntj922dj4crc4pre4e0xt3dyj0t5rsw9uw0tus"
                        },
                        {
                            "amount":50000,
                            "address": "avax1yk0xzmqyyaxn26sqceuky2tc2fh2q327vcwvda"
                        }
                    ]
                }
            },
            "asset2": {
                "name": "myVarCapAsset",
                "symbol":"MVCA",
                "initialState": {
                    "variableCap" : [
                        {
                            "minters": [
                                "avax1kcfg6avc94ct3qh2mtdg47thsk8nrflnrgwjqr",
                                "avax14e2s22wxvf3c7309txxpqs0qe9tjwwtk0dme8e"
                            ],
                            "threshold":1
                        },
                        {
                            "minters": [
                                "avax1y8pveyn82gjyqr7kqzp72pqym6xlch9gt5grck",
                                "avax1c5cmm0gem70rd8dcnpel63apzfnfxye9kd4wwe",
                                "avax12euam2lwtwa8apvfdl700ckhg86euag2hlhmyw"
                            ],
                            "threshold":2
                        }
                    ]
                }
            }
        },
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/vm/avm
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "bytes": "0x0000000000010006617373657431000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000f6d794669786564436170417373657400044d464341000000000100000000000000010000000700000000000186a10000000000000000000000010000000152b219bc1b9ab0a9f2e3f9216e4460bd5db8d153bfa57c3c",
    "encoding": "hex"
  },
  "id": 1
}
```

### `avm.createAddress`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Create a new address controlled by the given user.

**Signature:**

```sh
avm.createAddress({
    username: string,
    password: string
}) -> {address: string}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "avm.createAddress",
    "params": {
        "username": "myUsername",
        "password": "myPassword"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "address": "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"
  },
  "id": 1
}
```

<!--
TODO: Add avm.createAsset
-->

### `avm.createFixedCapAsset`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Create a new fixed-cap, fungible asset. A quantity of it is created at initialization and then no
more is ever created. The asset can be sent with `avm.send`.

**Signature:**

```sh
avm.createFixedCapAsset({
    name: string,
    symbol: string,
    denomination: int, //optional
    initialHolders: []{
        address: string,
        amount: int
    },
    from: []string, //optional
    changeAddr: string, //optional
    username: string,
    password: string
}) ->
{
    assetID: string,
    changeAddr: string
}
```

- `name` is a human-readable name for the asset. Not necessarily unique.
- `symbol` is a shorthand symbol for the asset. Between 0 and 4 characters. Not necessarily unique.
  May be omitted.
- `denomination` determines how balances of this asset are displayed by user interfaces. If
  `denomination` is 0, 100 units of this asset are displayed as 100. If `denomination` is 1, 100
  units of this asset are displayed as 10.0. If `denomination` is 2, 100 units of this asset are
  displayed as 1.00, etc. Defaults to 0.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- `username` and `password` denote the user paying the transaction fee.
- Each element in `initialHolders` specifies that `address` holds `amount` units of the asset at
  genesis.
- `assetID` is the ID of the new asset.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"avm.createFixedCapAsset",
    "params" :{
        "name": "myFixedCapAsset",
        "symbol":"MFCA",
        "initialHolders": [
            {
                "address": "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
                "amount": 10000
            },
            {
                "address":"X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
                "amount":50000
            }
        ],
        "from":["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr":"X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "assetID": "ZiKfqRXCZgHLgZ4rxGU9Qbycdzuq5DRY4tdSNS9ku8kcNxNLD",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  }
}
```

### `avm.createNFTAsset`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Create a new non-fungible asset. No units of the asset exist at initialization. Minters can mint
units of this asset using `avm.mintNFT`.

**Signature:**

```sh
avm.createNFTAsset({
    name: string,
    symbol: string,
    minterSets: []{
        minters: []string,
        threshold: int
    },
    from: []string, //optional
    changeAddr: string, //optional
    username: string,
    password: string
}) ->
 {
    assetID: string,
    changeAddr: string,
}
```

- `name` is a human-readable name for the asset. Not necessarily unique.
- `symbol` is a shorthand symbol for the asset. Between 0 and 4 characters. Not necessarily unique.
  May be omitted.
- `minterSets` is a list where each element specifies that `threshold` of the addresses in `minters`
  may together mint more of the asset by signing a minting transaction.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- `username` pays the transaction fee.
- `assetID` is the ID of the new asset.
- `changeAddr` in the result is the address where any change was sent.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"avm.createNFTAsset",
    "params" :{
        "name":"Coincert",
        "symbol":"TIXX",
        "minterSets":[
            {
                "minters":[
                    "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
                ],
                "threshold": 1
            }
        ],
        "from": ["X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"],
        "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "assetID": "2KGdt2HpFKpTH5CtGZjYt5XPWs6Pv9DLoRBhiFfntbezdRvZWP",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  },
  "id": 1
}
```

### `avm.createVariableCapAsset`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Create a new variable-cap, fungible asset. No units of the asset exist at initialization. Minters
can mint units of this asset using `avm.mint`.

**Signature:**

```sh
avm.createVariableCapAsset({
    name: string,
    symbol: string,
    denomination: int, //optional
    minterSets: []{
        minters: []string,
        threshold: int
    },
    from: []string, //optional
    changeAddr: string, //optional
    username: string,
    password: string
}) ->
{
    assetID: string,
    changeAddr: string,
}
```

- `name` is a human-readable name for the asset. Not necessarily unique.
- `symbol` is a shorthand symbol for the asset. Between 0 and 4 characters. Not necessarily unique.
  May be omitted.
- `denomination` determines how balances of this asset are displayed by user interfaces. If
  denomination is 0, 100 units of this asset are displayed as 100. If denomination is 1, 100 units
  of this asset are displayed as 10.0. If denomination is 2, 100 units of this asset are displays as
  .100, etc.
- `minterSets` is a list where each element specifies that `threshold` of the addresses in `minters`
  may together mint more of the asset by signing a minting transaction.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- `username` pays the transaction fee.
- `assetID` is the ID of the new asset.
- `changeAddr` in the result is the address where any change was sent.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"avm.createVariableCapAsset",
    "params" :{
        "name":"myVariableCapAsset",
        "symbol":"MFCA",
        "minterSets":[
            {
                "minters":[
                    "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"
                ],
                "threshold": 1
            },
            {
                "minters": [
                    "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
                    "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
                    "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"
                ],
                "threshold": 2
            }
        ],
        "from":["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr":"X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "assetID": "2QbZFE7J4MAny9iXHUwq8Pz8SpFhWk3maCw4SkinVPv6wPmAbK",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  }
}
```

### `avm.export`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Send an asset from the X-Chain to the P-Chain or C-Chain.

**Signature:**

```sh
avm.export({
    to: string,
    amount: int,
    assetID: string,
    from: []string, //optional
    changeAddr: string, //optional
    username: string,
    password: string,
}) ->
{
    txID: string,
    changeAddr: string,
}
```

- `to` is the P-Chain or C-Chain address the asset is sent to.
- `amount` is the amount of the asset to send.
- `assetID` is the asset id of the asset which is sent. Use `AVAX` for AVAX exports.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- The asset is sent from addresses controlled by `username`
- `password` is `username`‘s password.

- `txID` is this transaction’s ID.
- `changeAddr` in the result is the address where any change was sent.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.export",
    "params" :{
        "to":"C-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
        "amount": 10,
        "assetID": "AVAX",
        "from":["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr":"X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txID": "2Eu16yNaepP57XrrJgjKGpiEDandpiGWW8xbUm6wcTYny3fejj",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  },
  "id": 1
}
```

<!--
TODO: Add avm.exportAVAX
-->

### `avm.exportKey`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Get the private key that controls a given address. The returned private key can be added to a user
with [`avm.importKey`](/reference/avalanchego/x-chain/api.md#avmimportkey).

**Signature:**

```sh
avm.exportKey({
    username: string,
    password: string,
    address: string
}) -> {privateKey: string}
```

- `username` must control `address`.
- `privateKey` is the string representation of the private key that controls `address`.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.exportKey",
    "params" :{
        "username":"myUsername",
        "password":"myPassword",
        "address":"X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "privateKey": "PrivateKey-2w4XiXxPfQK4TypYqnohRL8DRNTz9cGiGmwQ1zmgEqD9c9KWLq"
  }
}
```

### `avm.getAddressTxs`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

Returns all transactions that change the balance of the given address. A transaction is said to
change an address's balance if either is true:

- A UTXO that the transaction consumes was at least partially owned by the address.
- A UTXO that the transaction produces is at least partially owned by the address.

:::tip
Note: Indexing (`index-transactions`) must be enabled in the X-chain config.
:::

**Signature:**

```sh
avm.getAddressTxs({
    address: string,
    cursor: uint64,     // optional, leave empty to get the first page
    assetID: string,
    pageSize: uint64    // optional, defaults to 1024
}) -> {
    txIDs: []string,
    cursor: uint64,
}
```

**Request Parameters:**

- `address`: The address for which we're fetching related transactions
- `assetID`: Only return transactions that changed the balance of this asset. Must be an ID or an
  alias for an asset.
- `pageSize`: Number of items to return per page. Optional. Defaults to 1024.

**Response Parameter:**

- `txIDs`: List of transaction IDs that affected the balance of this address.
- `cursor`: Page number or offset. Use this in request to get the next page.

**Example Call:**

```sh
curl -X POST --data '{
  "jsonrpc":"2.0",
  "id"     : 1,
  "method" :"avm.getAddressTxs",
  "params" :{
      "address":"X-local1kpprmfpzzm5lxyene32f6lr7j0aj7gxsu6hp9y",
      "assetID":"AVAX",
      "pageSize":20
  }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txIDs": ["SsJF7KKwxiUJkczygwmgLqo3XVRotmpKP8rMp74cpLuNLfwf6"],
    "cursor": "1"
  },
  "id": 1
}
```

### `avm.getAllBalances`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

Get the balances of all assets controlled by a given address.

**Signature:**

```sh
avm.getAllBalances({address:string}) -> {
    balances: []{
        asset: string,
        balance: int
    }
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"avm.getAllBalances",
    "params" :{
        "address":"X-avax1c79e0dd0susp7dc8udq34jgk2yvve7hapvdyht"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "balances": [
      {
        "asset": "AVAX",
        "balance": "102"
      },
      {
        "asset": "2sdnziCz37Jov3QSNMXcFRGFJ1tgauaj6L7qfk7yUcRPfQMC79",
        "balance": "10000"
      }
    ]
  },
  "id": 1
}
```

### `avm.getAssetDescription`

Get information about an asset.

**Signature:**

```sh
avm.getAssetDescription({assetID: string}) -> {
    assetId: string,
    name: string,
    symbol: string,
    denomination: int
}
```

- `assetID` is the id of the asset for which the information is requested.
- `name` is the asset’s human-readable, not necessarily unique name.
- `symbol` is the asset’s symbol.
- `denomination` determines how balances of this asset are displayed by user interfaces. If
  denomination is 0, 100 units of this asset are displayed as 100. If denomination is 1, 100 units
  of this asset are displayed as 10.0. If denomination is 2, 100 units of this asset are displays as
  .100, etc.

:::note

The AssetID for AVAX differs depending on the network you are on.

Mainnet: FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z

Testnet: U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK

For finding the `assetID` of other assets, this [info] might be useful.
Also, `avm.getUTXOs` returns the `assetID` in its output.

:::

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.getAssetDescription",
    "params" :{
        "assetID" :"FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
    "jsonrpc": "2.0",
    "result": {
        "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
        "name": "Avalanche",
        "symbol": "AVAX",
        "denomination": "9"
    },
    "id": 1
}`
```

### `avm.getBalance`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

Get the balance of an asset controlled by a given address.

**Signature:**

```sh
avm.getBalance({
    address: string,
    assetID: string
}) -> {balance: int}
```

- `address` owner of the asset
- `assetID` id of the asset for which the balance is requested

**Example Call:**

```sh
curl -X POST --data '{
  "jsonrpc":"2.0",
  "id"     : 1,
  "method" :"avm.getBalance",
  "params" :{
      "address":"X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
      "assetID": "2pYGetDWyKdHxpFxh2LHeoLNCH6H5vxxCxHQtFnnFaYxLsqtHC"
  }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "balance": "299999999999900",
    "utxoIDs": [
      {
        "txID": "WPQdyLNqHfiEKp4zcCpayRHYDVYuh1hqs9c1RqgZXS4VPgdvo",
        "outputIndex": 1
      }
    ]
  }
}
```

### `avm.getBlock`

Returns the block with the given id.

**Signature:**

```sh
avm.getBlock({
    blockID: string
    encoding: string // optional
}) -> {
    block: string,
    encoding: string
}
```

**Request:**

- `blockID` is the block ID. It should be in cb58 format.
- `encoding` is the encoding format to use. Can be either `hex` or `json`. Defaults to `hex`.

**Response:**

- `block` is the transaction encoded to `encoding`.
- `encoding` is the `encoding`.

#### Hex Example

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "avm.getBlock",
    "params": {
        "blockID": "tXJ4xwmR8soHE6DzRNMQPtiwQvuYsHn6eLLBzo2moDqBquqy6",
        "encoding": "hex"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "block": "0x00000000002000000000641ad33ede17f652512193721df87994f783ec806bb5640c39ee73676caffcc3215e0651000000000049a80a000000010000000e0000000100000000000000000000000000000000000000000000000000000000000000000000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000070000002e1a2a3910000000000000000000000001000000015cf998275803a7277926912defdf177b2e97b0b400000001e0d825c5069a7336671dd27eaa5c7851d2cf449e7e1cdc469c5c9e5a953955950000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000008908223b680000000100000000000000005e45d02fcc9e585544008f1df7ae5c94bf7f0f2600000000641ad3b600000000642d48b60000005aedf802580000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000070000005aedf80258000000000000000000000001000000015cf998275803a7277926912defdf177b2e97b0b40000000b000000000000000000000001000000012892441ba9a160bcdc596dcd2cc3ad83c3493589000000010000000900000001adf2237a5fe2dfd906265e8e14274aa7a7b2ee60c66213110598ba34fb4824d74f7760321c0c8fb1e8d3c5e86909248e48a7ae02e641da5559351693a8a1939800286d4fa2",
    "encoding": "hex"
  },
  "id": 1
}
```

### `avm.getBlockByHeight`

Returns block at the given height.

**Signature:**

```sh
avm.getBlockByHeight({
    height: string
    encoding: string // optional
}) -> {
    block: string,
    encoding: string
}
```

**Request:**

- `blockHeight` is the block height. It should be in `string` format.
- `encoding` is the encoding format to use. Can be either `hex` or `json`. Defaults to `hex`.

**Response:**

- `block` is the transaction encoded to `encoding`.
- `encoding` is the `encoding`.

#### Hex Example

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "avm.getBlockByHeight”,
    "params": {
        “height”: “275686313486”,
        "encoding": “hex”
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "block": "0x00000000002000000000642f6739d4efcdd07e4d4919a7fc2020b8a0f081dd64c262aaace5a6dad22be0b55fec0700000000004db9e100000001000000110000000100000000000000000000000000000000000000000000000000000000000000000000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000070000005c6ece390000000000000000000000000100000001930ab7bf5018bfc6f9435c8b15ba2fe1e619c0230000000000000000ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b00000001c6dda861341665c3b555b46227fb5e56dc0a870c5482809349f04b00348af2a80000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000005c6edd7b40000000010000000000000001000000090000000178688f4d5055bd8733801f9b52793da885bef424c90526c18e4dd97f7514bf6f0c3d2a0e9a5ea8b761bc41902eb4902c34ef034c4d18c3db7c83c64ffeadd93600731676de",
    "encoding": "hex"
  },
  "id": 1
}
```

### `avm.getHeight`

Returns the height of the last accepted block.

**Signature:**

```sh
avm.getHeight() ->
{
    height: uint64,
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "avm.getHeight",
    "params": {},
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "height": "5094088"
  },
  "id": 1
}
```

### `avm.getTx`

Returns the specified transaction. The `encoding` parameter sets the format of the returned
transaction. Can be either `"hex"` or `"json"`. Defaults to `"hex"`.

**Signature:**

```sh
avm.getTx({
    txID: string,
    encoding: string, //optional
}) -> {
    tx: string,
    encoding: string,
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.getTx",
    "params" :{
        "txID":"2oJCbb8pfdxEHAf9A8CdN4Afj9VSR3xzyzNkf8tDv7aM1sfNFL",
        "encoding": "json"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "tx": {
      "unsignedTx": {
        "networkID": 1,
        "blockchainID": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
        "outputs": [],
        "inputs": [
          {
            "txID": "2jbZUvi6nHy3Pgmk8xcMpSg5cW6epkPqdKkHSCweb4eRXtq4k9",
            "outputIndex": 1,
            "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
            "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
            "input": {
              "amount": 2570382395,
              "signatureIndices": [0]
            }
          }
        ],
        "memo": "0x",
        "destinationChain": "11111111111111111111111111111111LpoYY",
        "exportedOutputs": [
          {
            "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
            "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
            "output": {
              "addresses": ["X-avax1tnuesf6cqwnjw7fxjyk7lhch0vhf0v95wj5jvy"],
              "amount": 2569382395,
              "locktime": 0,
              "threshold": 1
            }
          }
        ]
      },
      "credentials": [
        {
          "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
          "credential": {
            "signatures": [
              "0x46ebcbcfbee3ece1fd15015204045cf3cb77f42c48d0201fc150341f91f086f177cfca8894ca9b4a0c55d6950218e4ea8c01d5c4aefb85cd7264b47bd57d224400"
            ]
          }
        }
      ],
      "id": "2oJCbb8pfdxEHAf9A8CdN4Afj9VSR3xzyzNkf8tDv7aM1sfNFL"
    },
    "encoding": "json"
  },
  "id": 1
}
```

Where:

- `credentials` is a list of this transaction's credentials. Each credential proves that this
  transaction's creator is allowed to consume one of this transaction's inputs. Each credential is a
  list of signatures.
- `unsignedTx` is the non-signature portion of the transaction.
- `networkID` is the ID of the network this transaction happened on. (Avalanche Mainnet is `1`.)
- `blockchainID` is the ID of the blockchain this transaction happened on. (Avalanche Mainnet
  X-Chain is `2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM`.)
- Each element of `outputs` is an output (UTXO) of this transaction that is not being exported to
  another chain.
- Each element of `inputs` is an input of this transaction which has not been imported from another
  chain.
- Import Transactions have additional fields `sourceChain` and `importedInputs`, which specify the
  blockchain ID that assets are being imported from, and the inputs that are being imported.
- Export Transactions have additional fields `destinationChain` and `exportedOutputs`, which specify
  the blockchain ID that assets are being exported to, and the UTXOs that are being exported.

An output contains:

- `assetID`: The ID of the asset being transferred. (The Mainnet Avax ID is
  `FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z`.)
- `fxID`: The ID of the FX this output uses.
- `output`: The FX-specific contents of this output.

Most outputs use the secp256k1 FX, look like this:

```json
{
  "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
  "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
  "output": {
    "addresses": ["X-avax126rd3w35xwkmj8670zvf7y5r8k36qa9z9803wm"],
    "amount": 1530084210,
    "locktime": 0,
    "threshold": 1
  }
}
```

The above output can be consumed after Unix time `locktime` by a transaction that has signatures
from `threshold` of the addresses in `addresses`.

### `avm.getTxStatus`

:::caution
Deprecated as of **v1.10.0**.
:::

Get the status of a transaction sent to the network.

**Signature:**

```sh
avm.getTxStatus({txID: string}) -> {status: string}
```

`status` is one of:

- `Accepted`: The transaction is (or will be) accepted by every node
- `Processing`: The transaction is being voted on by this node
- `Rejected`: The transaction will never be accepted by any node in the network
- `Unknown`: The transaction hasn’t been seen by this node

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.getTxStatus",
    "params" :{
        "txID":"2QouvFWUbjuySRxeX5xMbNCuAaKWfbk5FeEa2JmoF85RKLk2dD"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "status": "Accepted"
  }
}
```

### `avm.getUTXOs`

Gets the UTXOs that reference a given address. If `sourceChain` is specified, then it will retrieve
the atomic UTXOs exported from that chain to the X Chain.

**Signature:**

```sh
avm.getUTXOs({
    addresses: []string,
    limit: int, //optional
    startIndex: { //optional
        address: string,
        utxo: string
    },
    sourceChain: string, //optional
    encoding: string //optional
}) -> {
    numFetched: int,
    utxos: []string,
    endIndex: {
        address: string,
        utxo: string
    },
    sourceChain: string, //optional
    encoding: string
}
```

- `utxos` is a list of UTXOs such that each UTXO references at least one address in `addresses`.
- At most `limit` UTXOs are returned. If `limit` is omitted or greater than 1024, it is set to 1024.
- This method supports pagination. `endIndex` denotes the last UTXO returned. To get the next set of
  UTXOs, use the value of `endIndex` as `startIndex` in the next call.
- If `startIndex` is omitted, will fetch all UTXOs up to `limit`.
- When using pagination (when `startIndex` is provided), UTXOs are not guaranteed to be unique
  across multiple calls. That is, a UTXO may appear in the result of the first call, and then again
  in the second call.
- When using pagination, consistency is not guaranteed across multiple calls. That is, the UTXO set
  of the addresses may have changed between calls.
- `encoding` sets the format for the returned UTXOs. Can only be `hex` when a value is provided.

#### **Example**

Suppose we want all UTXOs that reference at least one of
`X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5` and `X-avax1d09qn852zcy03sfc9hay2llmn9hsgnw4tp3dv6`.

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.getUTXOs",
    "params" :{
        "addresses":["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5", "X-avax1d09qn852zcy03sfc9hay2llmn9hsgnw4tp3dv6"],
        "limit":5,
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

This gives response:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "numFetched": "5",
    "utxos": [
      "0x0000a195046108a85e60f7a864bb567745a37f50c6af282103e47cc62f036cee404700000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f216c1f01765",
      "0x0000ae8b1b94444eed8de9a81b1222f00f1b4133330add23d8ac288bffa98b85271100000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f216473d042a",
      "0x0000731ce04b1feefa9f4291d869adc30a33463f315491e164d89be7d6d2d7890cfc00000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f21600dd3047",
      "0x0000b462030cc4734f24c0bc224cf0d16ee452ea6b67615517caffead123ab4fbf1500000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f216c71b387e",
      "0x000054f6826c39bc957c0c6d44b70f961a994898999179cc32d21eb09c1908d7167b00000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f2166290e79d"
    ],
    "endIndex": {
      "address": "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
      "utxo": "kbUThAUfmBXUmRgTpgD6r3nLj7rJUGho6xyht5nouNNypH45j"
    },
    "encoding": "hex"
  },
  "id": 1
}
```

Since `numFetched` is the same as `limit`, we can tell that there may be more UTXOs that were not
fetched. We call the method again, this time with `startIndex`:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :2,
    "method" :"avm.getUTXOs",
    "params" :{
        "addresses":["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "limit":5,
        "startIndex": {
            "address": "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
            "utxo": "kbUThAUfmBXUmRgTpgD6r3nLj7rJUGho6xyht5nouNNypH45j"
        },
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

This gives response:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "numFetched": "4",
    "utxos": [
      "0x000020e182dd51ee4dcd31909fddd75bb3438d9431f8e4efce86a88a684f5c7fa09300000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f21662861d59",
      "0x0000a71ba36c475c18eb65dc90f6e85c4fd4a462d51c5de3ac2cbddf47db4d99284e00000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f21665f6f83f",
      "0x0000925424f61cb13e0fbdecc66e1270de68de9667b85baa3fdc84741d048daa69fa00000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f216afecf76a",
      "0x000082f30327514f819da6009fad92b5dba24d27db01e29ad7541aa8e6b6b554615c00000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f216779c2d59"
    ],
    "endIndex": {
      "address": "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
      "utxo": "21jG2RfqyHUUgkTLe2tUp6ETGLriSDTW3th8JXFbPRNiSZ11jK"
    },
    "encoding": "hex"
  },
  "id": 1
}
```

Since `numFetched` is less than `limit`, we know that we are done fetching UTXOs and don’t need to
call this method again.

Suppose we want to fetch the UTXOs exported from the P Chain to the X Chain in order to build an
ImportTx. Then we need to call GetUTXOs with the `sourceChain` argument in order to retrieve the
atomic UTXOs:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.getUTXOs",
    "params" :{
        "addresses":["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5", "X-avax1d09qn852zcy03sfc9hay2llmn9hsgnw4tp3dv6"],
        "limit":5,
        "sourceChain": "P",
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

This gives response:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "numFetched": "1",
    "utxos": [
      "0x00001f989ffaf18a18a59bdfbf209342aa61c6a62a67e8639d02bb3c8ddab315c6fa0000000039c33a499ce4c33a3b09cdd2cfa01ae70dbf2d18b2d7d168524440e55d550088000000070011c304cd7eb5c0000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c83497819"
    ],
    "endIndex": {
      "address": "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
      "utxo": "2Sz2XwRYqUHwPeiKoRnZ6ht88YqzAF1SQjMYZQQaB5wBFkAqST"
    },
    "encoding": "hex"
  },
  "id": 1
}
```

### `avm.import`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Finalize a transfer of an asset from the P-Chain or C-Chain to the X-Chain.

**Signature:**

```sh
avm.import({
    to: string,
    sourceChain: string,
    username: string,
    password: string,
}) -> {txID: string}
```

- `to` is the address the AVAX is sent to. This must be the same as the `to` argument in the
  corresponding call to the P-Chain’s `exportAVAX` or C-Chain's `export`.
- `sourceChain` is the ID or alias of the chain the AVAX is being imported from. To import funds
  from the C-Chain, use `"C"`.
- `username` is the user that controls `to`.
- `txID` is the ID of the newly created atomic transaction.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.import",
    "params" :{
        "to":"X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
        "sourceChain":"C",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txID": "2gXpf4jFoMAWQ3rxBfavgFfSdLkL2eFUYprKsUQuEdB5H6Jo1H"
  },
  "id": 1
}
```

<!--
TODO: Add avm.importAVAX
-->

### `avm.importKey`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Give a user control over an address by providing the private key that controls the address.

**Signature:**

```sh
avm.importKey({
    username: string,
    password: string,
    privateKey: string
}) -> {address: string}
```

- Add `privateKey` to `username`‘s set of private keys. `address` is the address `username` now
  controls with the private key.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.importKey",
    "params" :{
        "username":"myUsername",
        "password":"myPassword",
        "privateKey":"PrivateKey-2w4XiXxPfQK4TypYqnohRL8DRNTz9cGiGmwQ1zmgEqD9c9KWLq"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "address": "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"
  }
}
```

### `avm.issueTx`

Send a signed transaction to the network. `encoding` specifies the format of the signed transaction.
Can only be `hex` when a value is provided.

**Signature:**

```sh
avm.issueTx({
    tx: string,
    encoding: string, //optional
}) -> {
    txID: string
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"avm.issueTx",
    "params" :{
        "tx":"0x00000009de31b4d8b22991d51aa6aa1fc733f23a851a8c9400000000000186a0000000005f041280000000005f9ca900000030390000000000000001fceda8f90fcb5d30614b99d79fc4baa29307762668f16eb0259a57c2d3b78c875c86ec2045792d4df2d926c40f829196e0bb97ee697af71f5b0a966dabff749634c8b729855e937715b0e44303fd1014daedc752006011b730",
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "txID": "NUPLwbt2hsYxpQg4H2o451hmTWQ4JZx2zMzM4SinwtHgAdX1JLPHXvWSXEnpecStLj"
  }
}
```

### `avm.listAddresses`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

List addresses controlled by the given user.

**Signature:**

```sh
avm.listAddresses({
    username: string,
    password: string
}) -> {addresses: []string}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "avm.listAddresses",
    "params": {
        "username":"myUsername",
        "password":"myPassword"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "addresses": ["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"]
  },
  "id": 1
}
```

### `avm.mint`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Mint units of a variable-cap asset created with
[`avm.createVariableCapAsset`](/reference/avalanchego/x-chain/api.md#avmcreatevariablecapasset).

**Signature:**

```sh
avm.mint({
    amount: int,
    assetID: string,
    to: string,
    from: []string, //optional
    changeAddr: string, //optional
    username: string,
    password: string
}) ->
{
    txID: string,
    changeAddr: string,
}
```

- `amount` units of `assetID` will be created and controlled by address `to`.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- `username` is the user that pays the transaction fee. `username` must hold keys giving it
  permission to mint more of this asset. That is, it must control at least _threshold_ keys for one
  of the minter sets.
- `txID` is this transaction’s ID.
- `changeAddr` in the result is the address where any change was sent.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"avm.mint",
    "params" :{
        "amount":10000000,
        "assetID":"i1EqsthjiFTxunrj8WD2xFSrQ5p2siEKQacmCCB5qBFVqfSL2",
        "to":"X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
        "from":["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr":"X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "txID": "2oGdPdfw2qcNUHeqjw8sU2hPVrFyNUTgn6A8HenDra7oLCDtja",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  }
}
```

### `avm.mintNFT`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Mint non-fungible tokens which were created with
[`avm.createNFTAsset`](/reference/avalanchego/x-chain/api.md#avmcreatenftasset).

**Signature:**

```sh
avm.mintNFT({
    assetID: string,
    payload: string,
    to: string,
    encoding: string, //optional
    from: []string, //optional
    changeAddr: string, //optional
    username: string,
    password: string
}) ->
{
    txID: string,
    changeAddr: string,
}
```

- `assetID` is the assetID of the newly created NFT asset.
- `payload` is an arbitrary payload of up to 1024 bytes. Its encoding format is specified by the
  `encoding` argument.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- `username` is the user that pays the transaction fee. `username` must hold keys giving it
  permission to mint more of this asset. That is, it must control at least _threshold_ keys for one
  of the minter sets.
- `txID` is this transaction’s ID.
- `changeAddr` in the result is the address where any change was sent.
- `encoding` is the encoding format to use for the payload argument. Can only be `hex` when a value
  is provided.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"avm.mintNFT",
    "params" :{
        "assetID":"2KGdt2HpFKpTH5CtGZjYt5XPWs6Pv9DLoRBhiFfntbezdRvZWP",
        "payload":"0x415641204c61627338259aed",
        "to":"X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
        "from":["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr":"X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "txID": "2oGdPdfw2qcNUHeqjw8sU2hPVrFyNUTgn6A8HenDra7oLCDtja",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  }
}
```

### `avm.send`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Send a quantity of an asset to an address.

**Signature:**

```sh
avm.send({
    amount: int,
    assetID: string,
    to: string,
    memo: string, //optional
    from: []string, //optional
    changeAddr: string, //optional
    username: string,
    password: string
}) -> {txID: string, changeAddr: string}
```

- Sends `amount` units of asset with ID `assetID` to address `to`. `amount` is denominated in the
  smallest increment of the asset. For AVAX this is 1 nAVAX (one billionth of 1 AVAX.)
- `to` is the X-Chain address the asset is sent to.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- You can attach a `memo`, whose length can be up to 256 bytes.
- The asset is sent from addresses controlled by user `username`. (Of course, that user will need to
  hold at least the balance of the asset being sent.)

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.send",
    "params" :{
        "assetID"   : "AVAX",
        "amount"    : 10000,
        "to"        : "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
        "from"      : ["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "memo"      : "hi, mom!",
        "username"  : "userThatControlsAtLeast10000OfThisAsset",
        "password"  : "myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "txID": "2iXSVLPNVdnFqn65rRvLrsu8WneTFqBJRMqkBJx5vZTwAQb8c1",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  }
}
```

### `avm.sendMultiple`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Sends multiple transfers of `amount` of `assetID`, to a specified address from a list of owned
addresses.

**Signature:**

```sh
avm.sendMultiple({
    outputs: []{
      assetID: string,
      amount: int,
      to: string
    },
    from: []string, //optional
    changeAddr: string, //optional
    memo: string, //optional
    username: string,
    password: string
}) -> {txID: string, changeAddr: string}
```

- `outputs` is an array of object literals which each contain an `assetID`, `amount` and `to`.
- `memo` is an optional message, whose length can be up to 256 bytes.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- The asset is sent from addresses controlled by user `username`. (Of course, that user will need to
  hold at least the balance of the asset being sent.)

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.sendMultiple",
    "params" :{
        "outputs": [
            {
                "assetID" : "AVAX",
                "to"      : "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
                "amount"  : 1000000000
            },
            {
                "assetID" : "26aqSTpZuWDAVtRmo44fjCx4zW6PDEx3zy9Qtp2ts1MuMFn9FB",
                "to"      : "X-avax18knvhxx8uhc0mwlgrfyzjcm2wrd6e60w37xrjq",
                "amount"  : 10
            }
        ],
        "memo"      : "hi, mom!",
        "from"      : ["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username"  : "username",
        "password"  : "myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "txID": "2iXSVLPNVdnFqn65rRvLrsu8WneTFqBJRMqkBJx5vZTwAQb8c1",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  }
}
```

### `avm.sendNFT`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Send a non-fungible token.

**Signature:**

```sh
avm.sendNFT({
    assetID: string,
    groupID: number,
    to: string,
    from: []string, //optional
    changeAddr: string, //optional
    username: string,
    password: string
}) -> {txID: string}
```

- `assetID` is the asset ID of the NFT being sent.
- `groupID` is the NFT group from which to send the NFT. NFT creation allows multiple groups under
  each NFT ID. You can issue multiple NFTs to each group.
- `to` is the X-Chain address the NFT is sent to.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed. `changeAddr` is the address any change will be sent to. If omitted, change is
  sent to one of the addresses controlled by the user.
- The asset is sent from addresses controlled by user `username`. (Of course, that user will need to
  hold at least the balance of the NFT being sent.)

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.sendNFT",
    "params" :{
        "assetID"   : "2KGdt2HpFKpTH5CtGZjYt5XPWs6Pv9DLoRBhiFfntbezdRvZWP",
        "groupID"   : 0,
        "to"        : "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
        "from"      : ["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username"  : "myUsername",
        "password"  : "myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txID": "DoR2UtG1Trd3Q8gWXVevNxD666Q3DPqSFmBSMPQ9dWTV8Qtuy",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  },
  "id": 1
}
```

### `wallet.issueTx`

Send a signed transaction to the network and assume the TX will be accepted. `encoding` specifies
the format of the signed transaction. Can only be `hex` when a value is provided.

This call is made to the wallet API endpoint:

`/ext/bc/X/wallet`

:::caution

Endpoint deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

**Signature:**

```sh
wallet.issueTx({
    tx: string,
    encoding: string, //optional
}) -> {
    txID: string
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"wallet.issueTx",
    "params" :{
        "tx":"0x00000009de31b4d8b22991d51aa6aa1fc733f23a851a8c9400000000000186a0000000005f041280000000005f9ca900000030390000000000000001fceda8f90fcb5d30614b99d79fc4baa29307762668f16eb0259a57c2d3b78c875c86ec2045792d4df2d926c40f829196e0bb97ee697af71f5b0a966dabff749634c8b729855e937715b0e44303fd1014daedc752006011b730",
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X/wallet
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "txID": "NUPLwbt2hsYxpQg4H2o451hmTWQ4JZx2zMzM4SinwtHgAdX1JLPHXvWSXEnpecStLj"
  }
}
```

### `wallet.send`

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Send a quantity of an asset to an address and assume the TX will be accepted so that future calls
can use the modified UTXO set.

This call is made to the wallet API endpoint:

`/ext/bc/X/wallet`

:::caution

Endpoint deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

**Signature:**

```sh
wallet.send({
    amount: int,
    assetID: string,
    to: string,
    memo: string, //optional
    from: []string, //optional
    changeAddr: string, //optional
    username: string,
    password: string
}) -> {txID: string, changeAddr: string}
```

- Sends `amount` units of asset with ID `assetID` to address `to`. `amount` is denominated in the
  smallest increment of the asset. For AVAX this is 1 nAVAX (one billionth of 1 AVAX.)
- `to` is the X-Chain address the asset is sent to.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- You can attach a `memo`, whose length can be up to 256 bytes.
- The asset is sent from addresses controlled by user `username`. (Of course, that user will need to
  hold at least the balance of the asset being sent.)

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"wallet.send",
    "params" :{
        "assetID"   : "AVAX",
        "amount"    : 10000,
        "to"        : "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
        "memo"      : "hi, mom!",
        "from"      : ["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username"  : "userThatControlsAtLeast10000OfThisAsset",
        "password"  : "myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X/wallet
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "txID": "2iXSVLPNVdnFqn65rRvLrsu8WneTFqBJRMqkBJx5vZTwAQb8c1",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  }
}
```

### `wallet.sendMultiple`

:::warning
Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).
:::

Send multiple transfers of `amount` of `assetID`, to a specified address from a list of owned of
addresses and assume the TX will be accepted so that future calls can use the modified UTXO set.

This call is made to the wallet API endpoint:

`/ext/bc/X/wallet`

:::caution

Endpoint deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

**Signature:**

```sh
wallet.sendMultiple({
    outputs: []{
      assetID: string,
      amount: int,
      to: string
    },
    from: []string, //optional
    changeAddr: string, //optional
    memo: string, //optional
    username: string,
    password: string
}) -> {txID: string, changeAddr: string}
```

- `outputs` is an array of object literals which each contain an `assetID`, `amount` and `to`.
- `from` are the addresses that you want to use for this operation. If omitted, uses any of your
  addresses as needed.
- `changeAddr` is the address any change will be sent to. If omitted, change is sent to one of the
  addresses controlled by the user.
- You can attach a `memo`, whose length can be up to 256 bytes.
- The asset is sent from addresses controlled by user `username`. (Of course, that user will need to
  hold at least the balance of the asset being sent.)

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"wallet.sendMultiple",
    "params" :{
        "outputs": [
            {
                "assetID" : "AVAX",
                "to"      : "X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
                "amount"  : 1000000000
            },
            {
                "assetID" : "26aqSTpZuWDAVtRmo44fjCx4zW6PDEx3zy9Qtp2ts1MuMFn9FB",
                "to"      : "X-avax18knvhxx8uhc0mwlgrfyzjcm2wrd6e60w37xrjq",
                "amount"  : 10
            }
        ],
        "memo"      : "hi, mom!",
        "from"      : ["X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8",
        "username"  : "username",
        "password"  : "myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X/wallet
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "txID": "2iXSVLPNVdnFqn65rRvLrsu8WneTFqBJRMqkBJx5vZTwAQb8c1",
    "changeAddr": "X-avax1turszjwn05lflpewurw96rfrd3h6x8flgs5uf8"
  }
}
```

### Events

Listen for transactions on a specified address.

This call is made to the events API endpoint:

`/ext/bc/X/events`

:::caution

Endpoint deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

#### **Golang Example**

```go
package main

import (
    "encoding/json"
    "log"
    "net"
    "net/http"
    "sync"

    "github.com/ava-labs/avalanchego/api"
    "github.com/ava-labs/avalanchego/pubsub"
    "github.com/gorilla/websocket"
)

func main() {
    dialer := websocket.Dialer{
        NetDial: func(netw, addr string) (net.Conn, error) {
            return net.Dial(netw, addr)
        },
    }

    httpHeader := http.Header{}
    conn, _, err := dialer.Dial("ws://localhost:9650/ext/bc/X/events", httpHeader)
    if err != nil {
        panic(err)
    }

    waitGroup := &sync.WaitGroup{}
    waitGroup.Add(1)

    readMsg := func() {
        defer waitGroup.Done()

        for {
            mt, msg, err := conn.ReadMessage()
            if err != nil {
                log.Println(err)
                return
            }
            switch mt {
            case websocket.TextMessage:
                log.Println(string(msg))
            default:
                log.Println(mt, string(msg))
            }
        }
    }

    go readMsg()

    cmd := &pubsub.Command{NewSet: &pubsub.NewSet{}}
    cmdmsg, err := json.Marshal(cmd)
    if err != nil {
        panic(err)
    }
    err = conn.WriteMessage(websocket.TextMessage, cmdmsg)
    if err != nil {
        panic(err)
    }

    var addresses []string
    addresses = append(addresses, " X-fuji....")
    cmd = &pubsub.Command{AddAddresses: &pubsub.AddAddresses{JSONAddresses: api.JSONAddresses{Addresses: addresses}}}
    cmdmsg, err = json.Marshal(cmd)
    if err != nil {
        panic(err)
    }

    err = conn.WriteMessage(websocket.TextMessage, cmdmsg)
    if err != nil {
        panic(err)
    }

    waitGroup.Wait()
}
```

**Operations:**

| Command          | Description                  | Example                                                        | Arguments                                                                                                                              |
| :--------------- | :--------------------------- | :------------------------------------------------------------- | :------------------------------------------------------------------------------------------------------------------------------------- |
| **NewSet**       | create a new address map set | `{"newSet":{}}`                                                |                                                                                                                                        |
| **NewBloom**     | create a new bloom set.      | `{"newBloom":{"maxElements":"1000","collisionProb":"0.0100"}}` | `maxElements` - number of elements in filter must be &gt; 0 `collisionProb` - allowed collision probability must be &gt; 0 and &lt;= 1 |
| **AddAddresses** | add an address to the set    | `{"addAddresses":{"addresses":\["X-fuji..."\]}}`               | addresses - list of addresses to match                                                                                                 |

Calling **NewSet** or **NewBloom** resets the filter, and must be followed with **AddAddresses**.
**AddAddresses** can be called multiple times.

**Set details:**

- **NewSet** performs absolute address matches, if the address is in the set you will be sent the
  transaction.
- **NewBloom** [Bloom filtering](https://en.wikipedia.org/wiki/Bloom_filter) can produce false
  positives, but can allow a greater number of addresses to be filtered. If the addresses is in the
  filter, you will be sent the transaction.

**Example Response:**

```json
2021/05/11 15:59:35 {"txID":"22HWKHrREyXyAiDnVmGp3TQQ79tHSSVxA9h26VfDEzoxvwveyk"}
```
