The [X-Chain](https://build.avax.network/docs/quick-start/primary-network#x-chain),
Avalanche's native platform for creating and trading assets, is an instance of the Avalanche Virtual
Machine (AVM). This API allows clients to create and trade assets on the X-Chain and other instances
of the AVM.

## Format

This API uses the `json 2.0` RPC format. For more information on making JSON RPC calls, see
[here](https://build.avax.network/docs/api-reference/guides/issuing-api-calls).

## Endpoints

`/ext/bc/X` to interact with the X-Chain.

`/ext/bc/blockchainID` to interact with other AVM instances, where `blockchainID` is the ID of a
blockchain running the AVM.

## Methods

### `avm.getAllBalances`

<Callout type="warn">
Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).
</Callout>

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

<Callout type="note">
The AssetID for AVAX differs depending on the network you are on.

Mainnet: FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z

Testnet: U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK

For finding the `assetID` of other assets, this [info] might be useful.
Also, `avm.getUTXOs` returns the `assetID` in its output.
</Callout>

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

<Callout type="warn">
Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).
</Callout>

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
    "method": "avm.getBlockByHeight",
    "params": {
        "height": "275686313486",
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

### `avm.getTxFee`

Get the fees of the network.

**Signature**:

```
avm.getTxFee() ->
{
  txFee: uint64,
  createAssetTxFee: uint64,
}
```

- `txFee` is the default fee for making transactions.
- `createAssetTxFee` is the fee for creating a new asset.

All fees are denominated in nAVAX.

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"avm.getTxFee",
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txFee": "1000000",
    "createAssetTxFee": "10000000"
  }
}
```

### `avm.getTxStatus`

<Callout type="warn">
Deprecated as of **v1.10.0**.
</Callout>

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
    numFetched: string,
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
