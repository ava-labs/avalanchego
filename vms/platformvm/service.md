---
tags: [P-Chain, Platform Chain, AvalancheGo APIs]
description: This page is an overview of the P-Chain API associated with AvalancheGo.
sidebar_label: API
pagination_label: P-Chain Transaction Format
---

# Platform Chain API

This API allows clients to interact with the
[P-Chain](/learn/avalanche/avalanche-platform.md#p-chain), which
maintains Avalanche’s [validator](/nodes/validate/how-to-stake#validators) set and handles
blockchain creation.

## Endpoint

```sh
/ext/bc/P
```

## Format

This API uses the `json 2.0` RPC format.

## Methods

### `platform.exportKey`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning

Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).

:::

Get the private key that controls a given address.

**Signature:**

```sh
platform.exportKey({
    username: string,
    password: string,
    address: string
}) -> {privateKey: string}
```

- `username` is the user that controls `address`.
- `password` is `username`‘s password.
- `privateKey` is the string representation of the private key that controls `address`.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"platform.exportKey",
    "params" :{
        "username" :"myUsername",
        "password": "myPassword",
        "address": "P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "privateKey": "PrivateKey-Lf49kAJw3CbaL783vmbeAJvhscJqC7vi5yBYLxw2XfbzNS5RS"
  }
}
```

### `platform.getBalance`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

Get the balance of AVAX controlled by a given address.

**Signature:**

```sh
platform.getBalance({
    addresses: []string
}) -> {
    balances: string -> int,
    unlockeds: string -> int,
    lockedStakeables: string -> int,
    lockedNotStakeables: string -> int,
    utxoIDs: []{
        txID: string,
        outputIndex: int
    }
}
```

- `addresses` are the addresses to get the balance of.
- `balances` is a map from assetID to the total balance.
- `unlockeds` is a map from assetID to the unlocked balance.
- `lockedStakeables` is a map from assetID to the locked stakeable balance.
- `lockedNotStakeables` is a map from assetID to the locked and not stakeable balance.
- `utxoIDs` are the IDs of the UTXOs that reference `address`.

**Example Call:**

```sh
curl -X POST --data '{
  "jsonrpc":"2.0",
  "id"     : 1,
  "method" :"platform.getBalance",
  "params" :{
      "addresses":["P-custom18jma8ppw3nhx5r4ap8clazz0dps7rv5u9xde7p"]
  }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "balance": "30000000000000000",
    "unlocked": "20000000000000000",
    "lockedStakeable": "10000000000000000",
    "lockedNotStakeable": "0",
    "balances": {
      "BUuypiq2wyuLMvyhzFXcPyxPMCgSp7eeDohhQRqTChoBjKziC": "30000000000000000"
    },
    "unlockeds": {
      "BUuypiq2wyuLMvyhzFXcPyxPMCgSp7eeDohhQRqTChoBjKziC": "20000000000000000"
    },
    "lockedStakeables": {
      "BUuypiq2wyuLMvyhzFXcPyxPMCgSp7eeDohhQRqTChoBjKziC": "10000000000000000"
    },
    "lockedNotStakeables": {},
    "utxoIDs": [
      {
        "txID": "11111111111111111111111111111111LpoYY",
        "outputIndex": 1
      },
      {
        "txID": "11111111111111111111111111111111LpoYY",
        "outputIndex": 0
      }
    ]
  },
  "id": 1
}
```

### `platform.getBlock`

Get a block by its ID.

**Signature:**

```sh
platform.getBlock({
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

- `block` is the block encoded to `encoding`.
- `encoding` is the `encoding`.

#### Hex Example

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getBlock",
    "params": {
        "blockID": "d7WYmb8VeZNHsny3EJCwMm6QA37s1EHwMxw1Y71V3FqPZ5EFG",
        "encoding": "hex"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "block": "0x00000000000309473dc99a0851a29174d84e522da8ccb1a56ac23f7b0ba79f80acce34cf576900000000000f4241000000010000001200000001000000000000000000000000000000000000000000000000000000000000000000000000000000011c4c57e1bcb3c567f9f03caa75563502d1a21393173c06d9d79ea247b20e24800000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000338e0465f0000000100000000000000000427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd6520000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000070000000338d1041f0000000000000000000000010000000195a4467dd8f939554ea4e6501c08294386938cbf000000010000000900000001c79711c4b48dcde205b63603efef7c61773a0eb47efb503fcebe40d21962b7c25ebd734057400a12cce9cf99aceec8462923d5d91fffe1cb908372281ed738580119286dde",
    "encoding": "hex"
  },
  "id": 1
}
```

#### JSON Example

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getBlock",
    "params": {
        "blockID": "d7WYmb8VeZNHsny3EJCwMm6QA37s1EHwMxw1Y71V3FqPZ5EFG",
        "encoding": "json"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "block": {
      "parentID": "5615di9ytxujackzaXNrVuWQy5y8Yrt8chPCscMr5Ku9YxJ1S",
      "height": 1000001,
      "txs": [
        {
          "unsignedTx": {
            "inputs": {
              "networkID": 1,
              "blockchainID": "11111111111111111111111111111111LpoYY",
              "outputs": [],
              "inputs": [
                {
                  "txID": "DTqiagiMFdqbNQ62V2Gt1GddTVLkKUk2caGr4pyza9hTtsfta",
                  "outputIndex": 0,
                  "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
                  "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
                  "input": {
                    "amount": 13839124063,
                    "signatureIndices": [0]
                  }
                }
              ],
              "memo": "0x"
            },
            "destinationChain": "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5",
            "exportedOutputs": [
              {
                "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
                "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
                "output": {
                  "addresses": [
                    "P-avax1jkjyvlwclyu42n4yuegpczpfgwrf8r9lyj0d3c"
                  ],
                  "amount": 13838124063,
                  "locktime": 0,
                  "threshold": 1
                }
              }
            ]
          },
          "credentials": [
            {
              "signatures": [
                "0xc79711c4b48dcde205b63603efef7c61773a0eb47efb503fcebe40d21962b7c25ebd734057400a12cce9cf99aceec8462923d5d91fffe1cb908372281ed7385801"
              ]
            }
          ]
        }
      ]
    },
    "encoding": "json"
  },
  "id": 1
}
```

### `platform.getBlockByHeight`

Get a block by its height.

**Signature:**

```sh
platform.getBlockByHeight({
    height: int
    encoding: string // optional
}) -> {
    block: string,
    encoding: string
}
```

**Request:**

- `height` is the block height.
- `encoding` is the encoding format to use. Can be either `hex` or `json`. Defaults to `hex`.

**Response:**

- `block` is the block encoded to `encoding`.
- `encoding` is the `encoding`.

#### Hex Example

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getBlockByHeight",
    "params": {
        "height": 1000001,
        "encoding": "hex"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "block": "0x00000000000309473dc99a0851a29174d84e522da8ccb1a56ac23f7b0ba79f80acce34cf576900000000000f4241000000010000001200000001000000000000000000000000000000000000000000000000000000000000000000000000000000011c4c57e1bcb3c567f9f03caa75563502d1a21393173c06d9d79ea247b20e24800000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000050000000338e0465f0000000100000000000000000427d4b22a2a78bcddd456742caf91b56badbff985ee19aef14573e7343fd6520000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000070000000338d1041f0000000000000000000000010000000195a4467dd8f939554ea4e6501c08294386938cbf000000010000000900000001c79711c4b48dcde205b63603efef7c61773a0eb47efb503fcebe40d21962b7c25ebd734057400a12cce9cf99aceec8462923d5d91fffe1cb908372281ed738580119286dde",
    "encoding": "hex"
  },
  "id": 1
}
```

#### JSON Example

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getBlockByHeight",
    "params": {
        "height": 1000001,
        "encoding": "json"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "block": {
      "parentID": "5615di9ytxujackzaXNrVuWQy5y8Yrt8chPCscMr5Ku9YxJ1S",
      "height": 1000001,
      "txs": [
        {
          "unsignedTx": {
            "inputs": {
              "networkID": 1,
              "blockchainID": "11111111111111111111111111111111LpoYY",
              "outputs": [],
              "inputs": [
                {
                  "txID": "DTqiagiMFdqbNQ62V2Gt1GddTVLkKUk2caGr4pyza9hTtsfta",
                  "outputIndex": 0,
                  "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
                  "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
                  "input": {
                    "amount": 13839124063,
                    "signatureIndices": [0]
                  }
                }
              ],
              "memo": "0x"
            },
            "destinationChain": "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5",
            "exportedOutputs": [
              {
                "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
                "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
                "output": {
                  "addresses": [
                    "P-avax1jkjyvlwclyu42n4yuegpczpfgwrf8r9lyj0d3c"
                  ],
                  "amount": 13838124063,
                  "locktime": 0,
                  "threshold": 1
                }
              }
            ]
          },
          "credentials": [
            {
              "signatures": [
                "0xc79711c4b48dcde205b63603efef7c61773a0eb47efb503fcebe40d21962b7c25ebd734057400a12cce9cf99aceec8462923d5d91fffe1cb908372281ed7385801"
              ]
            }
          ]
        }
      ]
    },
    "encoding": "json"
  },
  "id": 1
}
```

### `platform.getBlockchains`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

Get all the blockchains that exist (excluding the P-Chain).

**Signature:**

```sh
platform.getBlockchains() ->
{
    blockchains: []{
        id: string,
        name:string,
        subnetID: string,
        vmID: string
    }
}
```

- `blockchains` is all of the blockchains that exists on the Avalanche network.
- `name` is the human-readable name of this blockchain.
- `id` is the blockchain’s ID.
- `subnetID` is the ID of the Subnet that validates this blockchain.
- `vmID` is the ID of the Virtual Machine the blockchain runs.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getBlockchains",
    "params": {},
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "blockchains": [
      {
        "id": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
        "name": "X-Chain",
        "subnetID": "11111111111111111111111111111111LpoYY",
        "vmID": "jvYyfQTxGMJLuGWa55kdP2p2zSUYsQ5Raupu4TW34ZAUBAbtq"
      },
      {
        "id": "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5",
        "name": "C-Chain",
        "subnetID": "11111111111111111111111111111111LpoYY",
        "vmID": "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6"
      },
      {
        "id": "CqhF97NNugqYLiGaQJ2xckfmkEr8uNeGG5TQbyGcgnZ5ahQwa",
        "name": "Simple DAG Payments",
        "subnetID": "11111111111111111111111111111111LpoYY",
        "vmID": "sqjdyTKUSrQs1YmKDTUbdUhdstSdtRTGRbUn8sqK8B6pkZkz1"
      },
      {
        "id": "VcqKNBJsYanhVFxGyQE5CyNVYxL3ZFD7cnKptKWeVikJKQkjv",
        "name": "Simple Chain Payments",
        "subnetID": "11111111111111111111111111111111LpoYY",
        "vmID": "sqjchUjzDqDfBPGjfQq2tXW1UCwZTyvzAWHsNzF2cb1eVHt6w"
      },
      {
        "id": "2SMYrx4Dj6QqCEA3WjnUTYEFSnpqVTwyV3GPNgQqQZbBbFgoJX",
        "name": "Simple Timestamp Server",
        "subnetID": "11111111111111111111111111111111LpoYY",
        "vmID": "tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH"
      },
      {
        "id": "KDYHHKjM4yTJTT8H8qPs5KXzE6gQH5TZrmP1qVr1P6qECj3XN",
        "name": "My new timestamp",
        "subnetID": "2bRCr6B4MiEfSjidDwxDpdCyviwnfUVqB2HGwhm947w9YYqb7r",
        "vmID": "tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH"
      },
      {
        "id": "2TtHFqEAAJ6b33dromYMqfgavGPF3iCpdG3hwNMiart2aB5QHi",
        "name": "My new AVM",
        "subnetID": "2bRCr6B4MiEfSjidDwxDpdCyviwnfUVqB2HGwhm947w9YYqb7r",
        "vmID": "jvYyfQTxGMJLuGWa55kdP2p2zSUYsQ5Raupu4TW34ZAUBAbtq"
      }
    ]
  },
  "id": 1
}
```

### `platform.getBlockchainStatus`

Get the status of a blockchain.

**Signature:**

```sh
platform.getBlockchainStatus(
    {
        blockchainID: string
    }
) -> {status: string}
```

`status` is one of:

- `Validating`: The blockchain is being validated by this node.
- `Created`: The blockchain exists but isn’t being validated by this node.
- `Preferred`: The blockchain was proposed to be created and is likely to be created but the
  transaction isn’t yet accepted.
- `Syncing`: This node is participating in this blockchain as a non-validating node.
- `Unknown`: The blockchain either wasn’t proposed or the proposal to create it isn’t preferred. The
  proposal may be resubmitted.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getBlockchainStatus",
    "params":{
        "blockchainID":"2NbS4dwGaf2p1MaXb65PrkZdXRwmSX4ZzGnUu7jm3aykgThuZE"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "status": "Created"
  },
  "id": 1
}
```

### `platform.getCurrentSupply`

Returns an upper bound on amount of tokens that exist that can stake the requested Subnet. This is
an upper bound because it does not account for burnt tokens, including transaction fees.

**Signature:**

```sh
platform.getCurrentSupply({
    subnetID: string // optional
}) -> {supply: int}
```

- `supply` is an upper bound on the number of tokens that exist.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getCurrentSupply",
    "params": {
        "subnetID": "11111111111111111111111111111111LpoYY"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "supply": "365865167637779183"
  },
  "id": 1
}
```

The response in this example indicates that AVAX’s supply is at most 365.865 million.

### `platform.getCurrentValidators`

List the current validators of the given Subnet.

**Signature:**

```sh
platform.getCurrentValidators({
    subnetID: string, // optional
    nodeIDs: string[], // optional
}) -> {
    validators: []{
        txID: string,
        startTime: string,
        endTime: string,
        stakeAmount: string,
        nodeID: string,
        weight: string,
        validationRewardOwner: {
            locktime: string,
            threshold: string,
            addresses: string[]
        },
        delegationRewardOwner: {
            locktime: string,
            threshold: string,
            addresses: string[]
        },
        potentialReward: string,
        delegationFee: string,
        uptime: string,
        connected: bool,
        signer: {
            publicKey: string,
            proofOfPosession: string
        },
        delegatorCount: string,
        delegatorWeight: string,
        delegators: []{
            txID: string,
            startTime: string,
            endTime: string,
            stakeAmount: string,
            nodeID: string,
            rewardOwner: {
                locktime: string,
                threshold: string,
                addresses: string[]
            },
            potentialReward: string,
        }
    }
}
```

- `subnetID` is the Subnet whose current validators are returned. If omitted, returns the current
  validators of the Primary Network.
- `nodeIDs` is a list of the NodeIDs of current validators to request. If omitted, all current
  validators are returned. If a specified NodeID is not in the set of current validators, it will
  not be included in the response.
- `validators`:
  - `txID` is the validator transaction.
  - `startTime` is the Unix time when the validator starts validating the Subnet.
  - `endTime` is the Unix time when the validator stops validating the Subnet.
  - `stakeAmount` is the amount of tokens this validator staked. Omitted if `subnetID` is not a PoS
    Subnet.
  - `nodeID` is the validator’s node ID.
  - `weight` is the validator’s weight when sampling validators. Omitted if `subnetID` is a PoS
    Subnet.
  - `validationRewardOwner` is an `OutputOwners` output which includes `locktime`, `threshold` and
    array of `addresses`. Specifies the owner of the potential reward earned from staking. Omitted
    if `subnetID` is not a PoS Subnet.
  - `delegationRewardOwner` is an `OutputOwners` output which includes `locktime`, `threshold` and
    array of `addresses`. Specifies the owner of the potential reward earned from delegations.
    Omitted if `subnetID` is not a PoS Subnet.
  - `potentialReward` is the potential reward earned from staking. Omitted if `subnetID` is not a
    PoS Subnet.
  - `delegationFeeRate` is the percent fee this validator charges when others delegate stake to
    them. Omitted if `subnetID` is not a PoS Subnet.
  - `uptime` is the % of time the queried node has reported the peer as online and validating the
    Subnet. Omitted if `subnetID` is not a PoS Subnet. (Deprecated: uptime is deprecated for Subnet Validators. It will be available only for Primary Network Validators.)
  - `connected` is if the node is connected and tracks the Subnet. (Deprecated: connected is deprecated for Subnet Validators. It will be available only for Primary Network Validators.)
  - `signer` is the node's BLS public key and proof of possession. Omitted if the validator doesn't
    have a BLS public key.
  - `delegatorCount` is the number of delegators on this validator.
    Omitted if `subnetID` is not a PoS Subnet.
  - `delegatorWeight` is total weight of delegators on this validator.
    Omitted if `subnetID` is not a PoS Subnet.
  - `delegators` is the list of delegators to this validator.
    Omitted if `subnetID` is not a PoS Subnet.
    Omitted unless `nodeIDs` specifies a single NodeID.
    - `txID` is the delegator transaction.
    - `startTime` is the Unix time when the delegator started.
    - `endTime` is the Unix time when the delegator stops.
    - `stakeAmount` is the amount of nAVAX this delegator staked.
    - `nodeID` is the validating node’s node ID.
    - `rewardOwner` is an `OutputOwners` output which includes `locktime`, `threshold` and array of
      `addresses`.
    - `potentialReward` is the potential reward earned from staking

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getCurrentValidators",
    "params": {
      "nodeIDs": ["NodeID-5mb46qkSBj81k9g9e4VFjGGSbaaSLFRzD"]
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "validators": [
      {
        "txID": "2NNkpYTGfTFLSGXJcHtVv6drwVU2cczhmjK2uhvwDyxwsjzZMm",
        "startTime": "1600368632",
        "endTime": "1602960455",
        "stakeAmount": "2000000000000",
        "nodeID": "NodeID-5mb46qkSBj81k9g9e4VFjGGSbaaSLFRzD",
        "validationRewardOwner": {
          "locktime": "0",
          "threshold": "1",
          "addresses": ["P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"]
        },
        "delegationRewardOwner": {
          "locktime": "0",
          "threshold": "1",
          "addresses": ["P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"]
        },
        "potentialReward": "117431493426",
        "delegationFee": "10.0000",
        "uptime": "0.0000",
        "connected": false,
        "delegatorCount": "1",
        "delegatorWeight": "25000000000",
        "delegators": [
          {
            "txID": "Bbai8nzGVcyn2VmeYcbS74zfjJLjDacGNVuzuvAQkHn1uWfoV",
            "startTime": "1600368523",
            "endTime": "1602960342",
            "stakeAmount": "25000000000",
            "nodeID": "NodeID-5mb46qkSBj81k9g9e4VFjGGSbaaSLFRzD",
            "rewardOwner": {
              "locktime": "0",
              "threshold": "1",
              "addresses": ["P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"]
            },
            "potentialReward": "11743144774"
          }
        ]
      }
    ]
  },
  "id": 1
}
```

### `platform.getHeight`

Returns the height of the last accepted block.

**Signature:**

```sh
platform.getHeight() ->
{
    height: int,
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getHeight",
    "params": {},
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "height": "56"
  },
  "id": 1
}
```

### `platform.getMaxStakeAmount`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

Returns the maximum amount of nAVAX staking to the named node during a particular time period.

**Signature:**

```sh
platform.getMaxStakeAmount(
    {
        subnetID: string,
        nodeID: string,
        startTime: int,
        endTime: int
    }
) ->
{
    amount: uint64
}
```

- `subnetID` is a Buffer or cb58 string representing Subnet
- `nodeID` is a string representing ID of the node whose stake amount is required during the given
  duration
- `startTime` is a big number denoting start time of the duration during which stake amount of the
  node is required.
- `endTime` is a big number denoting end time of the duration during which stake amount of the node
  is required.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getMaxStakeAmount",
    "params": {
        "subnetID":"11111111111111111111111111111111LpoYY",
        "nodeID":"NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg",
        "startTime": 1644240334,
        "endTime": 1644240634
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "amount": "2000000000000000"
  },
  "id": 1
}
```

### `platform.getMinStake`

Get the minimum amount of tokens required to validate the requested Subnet and the minimum amount of
tokens that can be delegated.

**Signature:**

```sh
platform.getMinStake({
    subnetID: string // optional
}) ->
{
    minValidatorStake : uint64,
    minDelegatorStake : uint64
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"platform.getMinStake",
    "params": {
        "subnetID":"11111111111111111111111111111111LpoYY"
    },
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "minValidatorStake": "2000000000000",
    "minDelegatorStake": "25000000000"
  },
  "id": 1
}
```

### `platform.getPendingValidators`

List the validators in the pending validator set of the specified Subnet. Each validator is not
currently validating the Subnet but will in the future.

**Signature:**

```sh
platform.getPendingValidators({
    subnetID: string, // optional
    nodeIDs: string[], // optional
}) -> {
    validators: []{
        txID: string,
        startTime: string,
        endTime: string,
        stakeAmount: string,
        nodeID: string,
        delegationFee: string,
        connected: bool,
        signer: {
            publicKey: string,
            proofOfPosession: string
        },
        weight: string,
    },
    delegators: []{
        txID: string,
        startTime: string,
        endTime: string,
        stakeAmount: string,
        nodeID: string
    }
}
```

- `subnetID` is the Subnet whose current validators are returned. If omitted, returns the current
  validators of the Primary Network.
- `nodeIDs` is a list of the NodeIDs of pending validators to request. If omitted, all pending
  validators are returned. If a specified NodeID is not in the set of pending validators, it will
  not be included in the response.
- `validators`:
  - `txID` is the validator transaction.
  - `startTime` is the Unix time when the validator starts validating the Subnet.
  - `endTime` is the Unix time when the validator stops validating the Subnet.
  - `stakeAmount` is the amount of tokens this validator staked. Omitted if `subnetID` is not a PoS
    Subnet.
  - `nodeID` is the validator’s node ID.
  - `connected` if the node is connected and tracks the Subnet.
  - `signer` is the node's BLS public key and proof of possession. Omitted if the validator doesn't
    have a BLS public key.
  - `weight` is the validator’s weight when sampling validators. Omitted if `subnetID` is a PoS
    Subnet.
- `delegators`:
  - `txID` is the delegator transaction.
  - `startTime` is the Unix time when the delegator starts.
  - `endTime` is the Unix time when the delegator stops.
  - `stakeAmount` is the amount of tokens this delegator staked.
  - `nodeID` is the validating node’s node ID.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getPendingValidators",
    "params": {},
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "validators": [
      {
        "txID": "2NNkpYTGfTFLSGXJcHtVv6drwVU2cczhmjK2uhvwDyxwsjzZMm",
        "startTime": "1600368632",
        "endTime": "1602960455",
        "stakeAmount": "200000000000",
        "nodeID": "NodeID-5mb46qkSBj81k9g9e4VFjGGSbaaSLFRzD",
        "delegationFee": "10.0000",
        "connected": false
      }
    ],
    "delegators": [
      {
        "txID": "Bbai8nzGVcyn2VmeYcbS74zfjJLjDacGNVuzuvAQkHn1uWfoV",
        "startTime": "1600368523",
        "endTime": "1602960342",
        "stakeAmount": "20000000000",
        "nodeID": "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg"
      }
    ]
  },
  "id": 1
}
```

### `platform.getRewardUTXOs`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

Returns the UTXOs that were rewarded after the provided transaction's staking or delegation period
ended.

**Signature:**

```sh
platform.getRewardUTXOs({
    txID: string,
    encoding: string // optional
}) -> {
    numFetched: integer,
    utxos: []string,
    encoding: string
}
```

- `txID` is the ID of the staking or delegating transaction
- `numFetched` is the number of returned UTXOs
- `utxos` is an array of encoded reward UTXOs
- `encoding` specifies the format for the returned UTXOs. Can only be `hex` when a value is
  provided.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getRewardUTXOs",
    "params": {
        "txID": "2nmH8LithVbdjaXsxVQCQfXtzN9hBbmebrsaEYnLM9T32Uy2Y5"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "numFetched": "2",
    "utxos": [
      "0x0000a195046108a85e60f7a864bb567745a37f50c6af282103e47cc62f036cee404700000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f216c1f01765",
      "0x0000ae8b1b94444eed8de9a81b1222f00f1b4133330add23d8ac288bffa98b85271100000000345aa98e8a990f4101e2268fab4c4e1f731c8dfbcffa3a77978686e6390d624f000000070000000000000001000000000000000000000001000000018ba98dabaebcd83056799841cfbc567d8b10f216473d042a"
    ],
    "encoding": "hex"
  },
  "id": 1
}
```

### `platform.getStake`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

Get the amount of nAVAX staked by a set of addresses. The amount returned does not include staking
rewards.

**Signature:**

```sh
platform.getStake({
    addresses: []string,
    validatorsOnly: true or false
}) ->
{
    stakeds: string -> int,
    stakedOutputs:  []string,
    encoding: string
}
```

- `addresses` are the addresses to get information about.
- `validatorsOnly` can be either `true` or `false`. If `true`, will skip checking delegators for stake.
- `stakeds` is a map from assetID to the amount staked by addresses provided.
- `stakedOutputs` are the string representation of staked outputs.
- `encoding` specifies the format for the returned outputs.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getStake",
    "params": {
        "addresses": [
            "P-avax1pmgmagjcljjzuz2ve339dx82khm7q8getlegte"
          ],
        "validatorsOnly": true
    },
    "id": 1
}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "staked": "6500000000000",
    "stakeds": {
      "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z": "6500000000000"
    },
    "stakedOutputs": [
      "0x000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000007000005e96630e800000000000000000000000001000000011f1c933f38da6ba0ba46f8c1b0a7040a9a991a80dd338ed1"
    ],
    "encoding": "hex"
  },
  "id": 1
}
```

### `platform.getStakingAssetID`

Retrieve an assetID for a Subnet’s staking asset.

**Signature:**

```sh
platform.getStakingAssetID({
    subnetID: string // optional
}) -> {
    assetID: string
}
```

- `subnetID` is the Subnet whose assetID is requested.
- `assetID` is the assetID for a Subnet’s staking asset.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getStakingAssetID",
    "params": {
        "subnetID": "11111111111111111111111111111111LpoYY"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "assetID": "2fombhL7aGPwj3KH4bfrmJwW6PVnMobf9Y2fn9GwxiAAJyFDbe"
  },
  "id": 1
}
```

:::note

The AssetID for AVAX differs depending on the network you are on.

Mainnet: FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z

Testnet: U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK

:::

### `platform.getSubnet`

Get owners and elastic info about the Subnet.

**Signature:**

```sh
platform.getSubnet({
    subnetID: string
}) ->
{
    isPermissioned: bool,
    controlKeys: []string,
    threshold: string,
    locktime: string,
    subnetTransformationTxID: string
}
```

- `subnetID` is the ID of the Subnet to get information about. If omitted, fails.
- `threshold` signatures from addresses in `controlKeys` are needed to make changes to
  a permissioned subnet. If the Subnet is a PoS Subnet, then `threshold` will be `0` and `controlKeys`
  will be empty.
- changes can not be made into the subnet until `locktime` is in the past.
- `subnetTransformationTxID` is the ID of the transaction that changed the subnet into a elastic one,
  for when this change was performed.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getSubnet",
    "params": {"subnetID":"Vz2ArUpigHt7fyE79uF3gAXvTPLJi2LGgZoMpgNPHowUZJxBb"},
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "isPermissioned": true,
    "controlKeys": ["P-fuji1ztvstx6naeg6aarfd047fzppdt8v4gsah88e0c","P-fuji193kvt4grqewv6ce2x59wnhydr88xwdgfcedyr3"],
    "threshold": "1",
    "locktime": "0",
    "subnetTransformationTxID": "11111111111111111111111111111111LpoYY"
  },
  "id": 1
}
```

### `platform.getSubnets`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

Get info about the Subnets.

**Signature:**

```sh
platform.getSubnets({
    ids: []string
}) ->
{
    subnets: []{
        id: string,
        controlKeys: []string,
        threshold: string
    }
}
```

- `ids` are the IDs of the Subnets to get information about. If omitted, gets information about all
  Subnets.
- `id` is the Subnet’s ID.
- `threshold` signatures from addresses in `controlKeys` are needed to add a validator to the
  Subnet. If the Subnet is a PoS Subnet, then `threshold` will be `0` and `controlKeys` will be
  empty.

See [here](/nodes/validate/add-a-validator.md) for information on adding a validator to a
Subnet.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getSubnets",
    "params": {"ids":["hW8Ma7dLMA7o4xmJf3AXBbo17bXzE7xnThUd3ypM4VAWo1sNJ"]},
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "subnets": [
      {
        "id": "hW8Ma7dLMA7o4xmJf3AXBbo17bXzE7xnThUd3ypM4VAWo1sNJ",
        "controlKeys": [
          "KNjXsaA1sZsaKCD1cd85YXauDuxshTes2",
          "Aiz4eEt5xv9t4NCnAWaQJFNz5ABqLtJkR"
        ],
        "threshold": "2"
      }
    ]
  },
  "id": 1
}
```

### `platform.getTimestamp`

Get the current P-Chain timestamp.

**Signature:**

```sh
platform.getTimestamp() -> {time: string}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getTimestamp",
    "params": {},
    "id": 1
}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "timestamp": "2021-09-07T00:00:00-04:00"
  },
  "id": 1
}
```

### `platform.getTotalStake`

Get the total amount of tokens staked on the requested Subnet.

**Signature:**

```sh
platform.getTotalStake({
    subnetID: string
}) -> {
    stake: int
    weight: int
}
```

#### Primary Network Example

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getTotalStake",
    "params": {
      "subnetID": "11111111111111111111111111111111LpoYY"
    },
    "id": 1
}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "stake": "279825917679866811",
    "weight": "279825917679866811"
  },
  "id": 1
}
```

#### Subnet Example

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getTotalStake",
    "params": {
        "subnetID": "2bRCr6B4MiEfSjidDwxDpdCyviwnfUVqB2HGwhm947w9YYqb7r",
    },
    "id": 1
}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "weight": "100000"
  },
  "id": 1
}
```

### `platform.getTx`

Gets a transaction by its ID.

Optional `encoding` parameter to specify the format for the returned transaction. Can be either
`hex` or `json`. Defaults to `hex`.

**Signature:**

```sh
platform.getTx({
    txID: string,
    encoding: string // optional
}) -> {
    tx: string,
    encoding: string,
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getTx",
    "params": {
        "txID":"28KVjSw5h3XKGuNpJXWY74EdnGq4TUWvCgEtJPymgQTvudiugb",
        "encoding": "json"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "tx": {
      "unsignedTx": {
        "networkID": 1,
        "blockchainID": "11111111111111111111111111111111LpoYY",
        "outputs": [],
        "inputs": [
          {
            "txID": "NXNJHKeaJyjjWVSq341t6LGQP5UNz796o1crpHPByv1TKp9ZP",
            "outputIndex": 0,
            "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
            "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
            "input": {
              "amount": 20824279595,
              "signatureIndices": [0]
            }
          },
          {
            "txID": "2ahK5SzD8iqi5KBqpKfxrnWtrEoVwQCqJsMoB9kvChCaHgAQC9",
            "outputIndex": 1,
            "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
            "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
            "input": {
              "amount": 28119890783,
              "signatureIndices": [0]
            }
          }
        ],
        "memo": "0x",
        "validator": {
          "nodeID": "NodeID-VT3YhgFaWEzy4Ap937qMeNEDscCammzG",
          "start": 1682945406,
          "end": 1684155006,
          "weight": 48944170378
        },
        "stake": [
          {
            "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
            "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
            "output": {
              "addresses": ["P-avax1tnuesf6cqwnjw7fxjyk7lhch0vhf0v95wj5jvy"],
              "amount": 48944170378,
              "locktime": 0,
              "threshold": 1
            }
          }
        ],
        "rewardsOwner": {
          "addresses": ["P-avax19zfygxaf59stehzedhxjesads0p5jdvfeedal0"],
          "locktime": 0,
          "threshold": 1
        }
      },
      "credentials": [
        {
          "signatures": [
            "0x6954e90b98437646fde0c1d54c12190fc23ae5e319c4d95dda56b53b4a23e43825251289cdc3728f1f1e0d48eac20e5c8f097baa9b49ea8a3cb6a41bb272d16601"
          ]
        },
        {
          "signatures": [
            "0x6954e90b98437646fde0c1d54c12190fc23ae5e319c4d95dda56b53b4a23e43825251289cdc3728f1f1e0d48eac20e5c8f097baa9b49ea8a3cb6a41bb272d16601"
          ]
        }
      ],
      "id": "28KVjSw5h3XKGuNpJXWY74EdnGq4TUWvCgEtJPymgQTvudiugb"
    },
    "encoding": "json"
  },
  "id": 1
}
```

### `platform.getTxStatus`

Gets a transaction’s status by its ID. If the transaction was dropped, response will include a
`reason` field with more information why the transaction was dropped.

**Signature:**

```sh
platform.getTxStatus({
    txID: string
}) -> {status: string}
```

`status` is one of:

- `Committed`: The transaction is (or will be) accepted by every node
- `Processing`: The transaction is being voted on by this node
- `Dropped`: The transaction will never be accepted by any node in the network, check `reason` field
  for more information
- `Unknown`: The transaction hasn’t been seen by this node

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getTxStatus",
    "params": {
        "txID":"TAG9Ns1sa723mZy1GSoGqWipK6Mvpaj7CAswVJGM6MkVJDF9Q"
   },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "status": "Committed"
  },
  "id": 1
}
```

### `platform.getUTXOs`

Gets the UTXOs that reference a given set of addresses.

**Signature:**

```sh
platform.getUTXOs(
    {
        addresses: []string,
        limit: int, // optional
        startIndex: { // optional
            address: string,
            utxo: string
        },
        sourceChain: string, // optional
        encoding: string, // optional
    },
) ->
{
    numFetched: int,
    utxos: []string,
    endIndex: {
        address: string,
        utxo: string
    },
    encoding: string,
}
```

- `utxos` is a list of UTXOs such that each UTXO references at least one address in `addresses`.
- At most `limit` UTXOs are returned. If `limit` is omitted or greater than 1024, it is set to 1024.
- This method supports pagination. `endIndex` denotes the last UTXO returned. To get the next set of
  UTXOs, use the value of `endIndex` as `startIndex` in the next call.
- If `startIndex` is omitted, will fetch all UTXOs up to `limit`.
- When using pagination (that is when `startIndex` is provided), UTXOs are not guaranteed to be unique
  across multiple calls. That is, a UTXO may appear in the result of the first call, and then again
  in the second call.
- When using pagination, consistency is not guaranteed across multiple calls. That is, the UTXO set
  of the addresses may have changed between calls.
- `encoding` specifies the format for the returned UTXOs. Can only be `hex` when a value is
  provided.

#### **Example**

Suppose we want all UTXOs that reference at least one of
`P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5` and `P-avax1d09qn852zcy03sfc9hay2llmn9hsgnw4tp3dv6`.

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"platform.getUTXOs",
    "params" :{
        "addresses":["P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5", "P-avax1d09qn852zcy03sfc9hay2llmn9hsgnw4tp3dv6"],
        "limit":5,
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
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
      "address": "P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
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
    "id"     :1,
    "method" :"platform.getUTXOs",
    "params" :{
        "addresses":["P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "limit":5,
        "startIndex": {
            "address": "P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
            "utxo": "0x62fc816bb209857923770c286192ab1f9e3f11e4a7d4ba0943111c3bbfeb9e4a5ea72fae"
        },
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
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
      "address": "P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
      "utxo": "21jG2RfqyHUUgkTLe2tUp6ETGLriSDTW3th8JXFbPRNiSZ11jK"
    },
    "encoding": "hex"
  },
  "id": 1
}
```

Since `numFetched` is less than `limit`, we know that we are done fetching UTXOs and don’t need to
call this method again.

Suppose we want to fetch the UTXOs exported from the X Chain to the P Chain in order to build an
ImportTx. Then we need to call GetUTXOs with the `sourceChain` argument in order to retrieve the
atomic UTXOs:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"platform.getUTXOs",
    "params" :{
        "addresses":["P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "sourceChain": "X",
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

This gives response:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "numFetched": "1",
    "utxos": [
      "0x00001f989ffaf18a18a59bdfbf209342aa61c6a62a67e8639d02bb3c8ddab315c6fa0000000139c33a499ce4c33a3b09cdd2cfa01ae70dbf2d18b2d7d168524440e55d55008800000007000000746a528800000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29cd704fe76"
    ],
    "endIndex": {
      "address": "P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
      "utxo": "S5UKgWoVpoGFyxfisebmmRf8WqC7ZwcmYwS7XaDVZqoaFcCwK"
    },
    "encoding": "hex"
  },
  "id": 1
}
```

### `platform.getValidatorsAt`

Get the validators and their weights of a Subnet or the Primary Network at a given P-Chain height.

**Signature:**

```sh
platform.getValidatorsAt(
    {
        height: int,
        subnetID: string, // optional
    }
)
```

- `height` is the P-Chain height to get the validator set at.
- `subnetID` is the Subnet ID to get the validator set of. If not given, gets validator set of the
  Primary Network.

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.getValidatorsAt",
    "params": {
        "height":1
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "validators": {
      "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg": 2000000000000000,
      "NodeID-GWPcbFJZFfZreETSoWjPimr846mXEKCtu": 2000000000000000,
      "NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ": 2000000000000000,
      "NodeID-NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN": 2000000000000000,
      "NodeID-P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5": 2000000000000000
    }
  },
  "id": 1
}
```

### `platform.issueTx`

Issue a transaction to the Platform Chain.

**Signature:**

```sh
platform.issueTx({
    tx: string,
    encoding: string, // optional
}) -> {txID: string}
```

- `tx` is the byte representation of a transaction.
- `encoding` specifies the encoding format for the transaction bytes. Can only be `hex` when a value
  is provided.
- `txID` is the transaction’s ID.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.issueTx",
    "params": {
        "tx":"0x00000009de31b4d8b22991d51aa6aa1fc733f23a851a8c9400000000000186a0000000005f041280000000005f9ca900000030390000000000000001fceda8f90fcb5d30614b99d79fc4baa29307762668f16eb0259a57c2d3b78c875c86ec2045792d4df2d926c40f829196e0bb97ee697af71f5b0a966dabff749634c8b729855e937715b0e44303fd1014daedc752006011b730",
        "encoding": "hex"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txID": "G3BuH6ytQ2averrLxJJugjWZHTRubzCrUZEXoheG5JMqL5ccY"
  },
  "id": 1
}
```

### `platform.listAddresses`

:::caution

Deprecated as of [**v1.9.12**](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12).

:::

:::warning

Not recommended for use on Mainnet. See warning notice in [Keystore API](/reference/avalanchego/keystore-api.md).

:::

List addresses controlled by the given user.

**Signature:**

```sh
platform.listAddresses({
    username: string,
    password: string
}) -> {addresses: []string}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.listAddresses",
    "params": {
        "username":"myUsername",
        "password":"myPassword"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "addresses": ["P-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"]
  },
  "id": 1
}
```

### `platform.sampleValidators`

Sample validators from the specified Subnet.

**Signature:**

```sh
platform.sampleValidators(
    {
        size: int,
        subnetID: string, // optional
    }
) ->
{
    validators: []string
}
```

- `size` is the number of validators to sample.
- `subnetID` is the Subnet to sampled from. If omitted, defaults to the Primary Network.
- Each element of `validators` is the ID of a validator.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"platform.sampleValidators",
    "params" :{
        "size":2
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "validators": [
      "NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ",
      "NodeID-NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN"
    ]
  }
}
```

### `platform.validatedBy`

Get the Subnet that validates a given blockchain.

**Signature:**

```sh
platform.validatedBy(
    {
        blockchainID: string
    }
) -> {subnetID: string}
```

- `blockchainID` is the blockchain’s ID.
- `subnetID` is the ID of the Subnet that validates the blockchain.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.validatedBy",
    "params": {
        "blockchainID": "KDYHHKjM4yTJTT8H8qPs5KXzE6gQH5TZrmP1qVr1P6qECj3XN"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "subnetID": "2bRCr6B4MiEfSjidDwxDpdCyviwnfUVqB2HGwhm947w9YYqb7r"
  },
  "id": 1
}
```

### `platform.validates`

Get the IDs of the blockchains a Subnet validates.

**Signature:**

```sh
platform.validates(
    {
        subnetID: string
    }
) -> {blockchainIDs: []string}
```

- `subnetID` is the Subnet’s ID.
- Each element of `blockchainIDs` is the ID of a blockchain the Subnet validates.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "platform.validates",
    "params": {
        "subnetID":"2bRCr6B4MiEfSjidDwxDpdCyviwnfUVqB2HGwhm947w9YYqb7r"
    },
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/P
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "blockchainIDs": [
      "KDYHHKjM4yTJTT8H8qPs5KXzE6gQH5TZrmP1qVr1P6qECj3XN",
      "2TtHFqEAAJ6b33dromYMqfgavGPF3iCpdG3hwNMiart2aB5QHi"
    ]
  },
  "id": 1
}
```
