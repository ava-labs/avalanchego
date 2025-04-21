AvalancheGo can be configured to run with an indexer. That is, it saves (indexes) every container (a block, vertex or transaction) it accepts on the X-Chain, P-Chain and C-Chain. To run AvalancheGo with indexing enabled, set command line flag [\--index-enabled](https://build.avax.network/docs/nodes/configure/configs-flags#--index-enabled-boolean) to true.

**AvalancheGo will only index containers that are accepted when running with `--index-enabled` set to true.** To ensure your node has a complete index, run a node with a fresh database and `--index-enabled` set to true. The node will accept every block, vertex and transaction in the network history during bootstrapping, ensuring your index is complete.

It is OK to turn off your node if it is running with indexing enabled. If it restarts with indexing still enabled, it will accept all containers that were accepted while it was offline. The indexer should never fail to index an accepted block, vertex or transaction.

Indexed containers (that is, accepted blocks, vertices and transactions) are timestamped with the time at which the node accepted that container. Note that if the container was indexed during bootstrapping, other nodes may have accepted the container much earlier. Every container indexed during bootstrapping will be timestamped with the time at which the node bootstrapped, not when it was first accepted by the network.

If `--index-enabled` is changed to `false` from `true`, AvalancheGo won't start as doing so would cause a previously complete index to become incomplete, unless the user explicitly says to do so with `--index-allow-incomplete`. This protects you from accidentally running with indexing disabled, after previously running with it enabled, which would result in an incomplete index.

This document shows how to query data from AvalancheGo's Index API. The Index API is only available when running with `--index-enabled`.

## Go Client

There is a Go implementation of an Index API client. See documentation [here](https://pkg.go.dev/github.com/ava-labs/avalanchego/indexer#Client). This client can be used inside a Go program to connect to an AvalancheGo node that is running with the Index API enabled and make calls to the Index API.

## Format

This API uses the `json 2.0` RPC format. For more information on making JSON RPC calls, see [here](https://build.avax.network/docs/api-reference/guides/issuing-api-calls).

## Endpoints

Each chain has one or more index. To see if a C-Chain block is accepted, for example, send an API call to the C-Chain block index. To see if an X-Chain vertex is accepted, for example, send an API call to the X-Chain vertex index.

### C-Chain Blocks

```
/ext/index/C/block
```

### P-Chain Blocks

```
/ext/index/P/block
```

### X-Chain Transactions

```
/ext/index/X/tx
```

### X-Chain Blocks

```
/ext/index/X/block
```

<Callout type="warn">
To ensure historical data can be accessed, the `/ext/index/X/vtx` is still accessible, even though it is no longer populated with chain data since the Cortina activation. If you are using `V1.10.0` or higher, you need to migrate to using the `/ext/index/X/block` endpoint.
</Callout>

## Methods

### `index.getContainerByID`

Get container by ID.

**Signature**:

```
index.getContainerByID({
  id: string,
  encoding: string
}) -> {
  id: string,
  bytes: string,
  timestamp: string,
  encoding: string,
  index: string
}
```

**Request**:

- `id` is the container's ID
- `encoding` is `"hex"` only.

**Response**:

- `id` is the container's ID
- `bytes` is the byte representation of the container
- `timestamp` is the time at which this node accepted the container
- `encoding` is `"hex"` only.
- `index` is how many containers were accepted in this index before this one

**Example Call**:

```sh
curl --location --request POST 'localhost:9650/ext/index/X/tx' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "index.getContainerByID",
    "params": {
        "id": "6fXf5hncR8LXvwtM8iezFQBpK5cubV6y1dWgpJCcNyzGB1EzY",
        "encoding":"hex"
    },
    "id": 1
}'
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "6fXf5hncR8LXvwtM8iezFQBpK5cubV6y1dWgpJCcNyzGB1EzY",
    "bytes": "0x00000000000400003039d891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070429ccc5c5eb3b80000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050429d069189e0000000000010000000000000000c85fc1980a77c5da78fe5486233fc09a769bb812bcb2cc548cf9495d046b3f1b00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000003a352a38240000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000100000009000000011cdb75d4e0b0aeaba2ebc1ef208373fedc1ebbb498f8385ad6fb537211d1523a70d903b884da77d963d56f163191295589329b5710113234934d0fd59c01676b00b63d2108",
    "timestamp": "2021-04-02T15:34:00.262979-07:00",
    "encoding": "hex",
    "index": "0"
  }
}
```

### `index.getContainerByIndex`

Get container by index. The first container accepted is at index 0, the second is at index 1, etc.

**Signature**:

```
index.getContainerByIndex({
  index: uint64,
  encoding: string
}) -> {
  id: string,
  bytes: string,
  timestamp: string,
  encoding: string,
  index: string
}
```

**Request**:

- `index` is how many containers were accepted in this index before this one
- `encoding` is `"hex"` only.

**Response**:

- `id` is the container's ID
- `bytes` is the byte representation of the container
- `timestamp` is the time at which this node accepted the container
- `index` is how many containers were accepted in this index before this one
- `encoding` is `"hex"` only.

**Example Call**:

```sh
curl --location --request POST 'localhost:9650/ext/index/X/tx' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "index.getContainerByIndex",
    "params": {
        "index":0,
        "encoding": "hex"
    },
    "id": 1
}'
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "6fXf5hncR8LXvwtM8iezFQBpK5cubV6y1dWgpJCcNyzGB1EzY",
    "bytes": "0x00000000000400003039d891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070429ccc5c5eb3b80000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050429d069189e0000000000010000000000000000c85fc1980a77c5da78fe5486233fc09a769bb812bcb2cc548cf9495d046b3f1b00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000003a352a38240000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000100000009000000011cdb75d4e0b0aeaba2ebc1ef208373fedc1ebbb498f8385ad6fb537211d1523a70d903b884da77d963d56f163191295589329b5710113234934d0fd59c01676b00b63d2108",
    "timestamp": "2021-04-02T15:34:00.262979-07:00",
    "encoding": "hex",
    "index": "0"
  }
}
```

### `index.getContainerRange`

Returns the transactions at index \[`startIndex`\], \[`startIndex+1`\], ... , \[`startIndex+n-1`\]

- If \[`n`\] == 0, returns an empty response (for example: null).
- If \[`startIndex`\] > the last accepted index, returns an error (unless the above apply.)
- If \[`n`\] > \[`MaxFetchedByRange`\], returns an error.
- If we run out of transactions, returns the ones fetched before running out.
- `numToFetch` must be in `[0,1024]`.

**Signature**:

```
index.getContainerRange({
  startIndex: uint64,
  numToFetch: uint64,
  encoding: string
}) -> []{
  id: string,
  bytes: string,
  timestamp: string,
  encoding: string,
  index: string
}
```

**Request**:

- `startIndex` is the beginning index
- `numToFetch` is the number of containers to fetch
- `encoding` is `"hex"` only.

**Response**:

- `id` is the container's ID
- `bytes` is the byte representation of the container
- `timestamp` is the time at which this node accepted the container
- `encoding` is `"hex"` only.
- `index` is how many containers were accepted in this index before this one

**Example Call**:

```sh
curl --location --request POST 'localhost:9650/ext/index/X/tx' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "index.getContainerRange",
    "params": {
        "startIndex":0,
        "numToFetch":100,
        "encoding": "hex"
    },
    "id": 1
}'
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": [
    {
      "id": "6fXf5hncR8LXvwtM8iezFQBpK5cubV6y1dWgpJCcNyzGB1EzY",
      "bytes": "0x00000000000400003039d891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070429ccc5c5eb3b80000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050429d069189e0000000000010000000000000000c85fc1980a77c5da78fe5486233fc09a769bb812bcb2cc548cf9495d046b3f1b00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000003a352a38240000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000100000009000000011cdb75d4e0b0aeaba2ebc1ef208373fedc1ebbb498f8385ad6fb537211d1523a70d903b884da77d963d56f163191295589329b5710113234934d0fd59c01676b00b63d2108",
      "timestamp": "2021-04-02T15:34:00.262979-07:00",
      "encoding": "hex",
      "index": "0"
    }
  ]
}
```

### `index.getIndex`

Get a container's index.

**Signature**:

```
index.getIndex({
  id: string,
  encoding: string
}) -> {
  index: string
}
```

**Request**:

- `id` is the ID of the container to fetch
- `encoding` is `"hex"` only.

**Response**:

- `index` is how many containers were accepted in this index before this one

**Example Call**:

```sh
curl --location --request POST 'localhost:9650/ext/index/X/tx' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "index.getIndex",
    "params": {
        "id":"6fXf5hncR8LXvwtM8iezFQBpK5cubV6y1dWgpJCcNyzGB1EzY",
        "encoding": "hex"
    },
    "id": 1
}'
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "index": "0"
  },
  "id": 1
}
```

### `index.getLastAccepted`

Get the most recently accepted container.

**Signature**:

```
index.getLastAccepted({
  encoding:string
}) -> {
  id: string,
  bytes: string,
  timestamp: string,
  encoding: string,
  index: string
}
```

**Request**:

- `encoding` is `"hex"` only.

**Response**:

- `id` is the container's ID
- `bytes` is the byte representation of the container
- `timestamp` is the time at which this node accepted the container
- `encoding` is `"hex"` only.

**Example Call**:

```sh
curl --location --request POST 'localhost:9650/ext/index/X/tx' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "index.getLastAccepted",
    "params": {
        "encoding": "hex"
    },
    "id": 1
}'
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "6fXf5hncR8LXvwtM8iezFQBpK5cubV6y1dWgpJCcNyzGB1EzY",
    "bytes": "0x00000000000400003039d891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070429ccc5c5eb3b80000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050429d069189e0000000000010000000000000000c85fc1980a77c5da78fe5486233fc09a769bb812bcb2cc548cf9495d046b3f1b00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000003a352a38240000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000100000009000000011cdb75d4e0b0aeaba2ebc1ef208373fedc1ebbb498f8385ad6fb537211d1523a70d903b884da77d963d56f163191295589329b5710113234934d0fd59c01676b00b63d2108",
    "timestamp": "2021-04-02T15:34:00.262979-07:00",
    "encoding": "hex",
    "index": "0"
  }
}
```

### `index.isAccepted`

Returns true if the container is in this index.

**Signature**:

```
index.isAccepted({
  id: string,
  encoding: string
}) -> {
  isAccepted: bool
}
```

**Request**:

- `id` is the ID of the container to fetch
- `encoding` is `"hex"` only.

**Response**:

- `isAccepted` displays if the container has been accepted

**Example Call**:

```sh
curl --location --request POST 'localhost:9650/ext/index/X/tx' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "index.isAccepted",
    "params": {
        "id":"6fXf5hncR8LXvwtM8iezFQBpK5cubV6y1dWgpJCcNyzGB1EzY",
        "encoding": "hex"
    },
    "id": 1
}'
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "isAccepted": true
  },
  "id": 1
}
```

## Example: Iterating Through X-Chain Transaction

Here is an example of how to iterate through all transactions on the X-Chain.

You can use the Index API to get the ID of every transaction that has been accepted on the X-Chain, and use the X-Chain API method `avm.getTx` to get a human-readable representation of the transaction.

To get an X-Chain transaction by its index (the order it was accepted in), use Index API method [index.getlastaccepted](#indexgetlastaccepted).

For example, to get the second transaction (note that `"index":1`) accepted on the X-Chain, do:

```sh
curl --location --request POST 'https://indexer-demo.avax.network/ext/index/X/tx' \
--header 'Content-Type: application/json' \
--data-raw '{
   "jsonrpc": "2.0",
   "method": "index.getContainerByIndex",
   "params": {
       "encoding":"hex",
       "index":1
   },
   "id": 1
}'
```

This returns the ID of the second transaction accepted in the X-Chain's history. To get the third transaction on the X-Chain, use `"index":2`, and so on.

The above API call gives the response below:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "id": "ZGYTSU8w3zUP6VFseGC798vA2Vnxnfj6fz1QPfA9N93bhjJvo",
    "bytes": "0x00000000000000000001ed5f38341e436e5d46e2bb00b45d62ae97d1b050c64bc634ae10626739e35c4b0000000221e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff000000070000000129f6afc0000000000000000000000001000000017416792e228a765c65e2d76d28ab5a16d18c342f21e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff0000000700000222afa575c00000000000000000000000010000000187d6a6dd3cd7740c8b13a410bea39b01fa83bb3e000000016f375c785edb28d52edb59b54035c96c198e9d80f5f5f5eee070592fe9465b8d0000000021e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff0000000500000223d9ab67c0000000010000000000000000000000010000000900000001beb83d3d29f1247efb4a3a1141ab5c966f46f946f9c943b9bc19f858bd416d10060c23d5d9c7db3a0da23446b97cd9cf9f8e61df98e1b1692d764c84a686f5f801a8da6e40",
    "timestamp": "2021-11-04T00:42:55.01643414Z",
    "encoding": "hex",
    "index": "1"
  },
  "id": 1
}
```

The ID of this transaction is `ZGYTSU8w3zUP6VFseGC798vA2Vnxnfj6fz1QPfA9N93bhjJvo`.

To get the transaction by its ID, use API method `avm.getTx`:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avm.getTx",
    "params" :{
        "txID":"ZGYTSU8w3zUP6VFseGC798vA2Vnxnfj6fz1QPfA9N93bhjJvo",
        "encoding": "json"
    }
}' -H 'content-type:application/json;' https://api.avax.network/ext/bc/X
```

**Response**:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "tx": {
      "unsignedTx": {
        "networkID": 1,
        "blockchainID": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
        "outputs": [
          {
            "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
            "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
            "output": {
              "addresses": ["X-avax1wst8jt3z3fm9ce0z6akj3266zmgccdp03hjlaj"],
              "amount": 4999000000,
              "locktime": 0,
              "threshold": 1
            }
          },
          {
            "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
            "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
            "output": {
              "addresses": ["X-avax1slt2dhfu6a6qezcn5sgtagumq8ag8we75f84sw"],
              "amount": 2347999000000,
              "locktime": 0,
              "threshold": 1
            }
          }
        ],
        "inputs": [
          {
            "txID": "qysTYUMCWdsR3MctzyfXiSvoSf6evbeFGRLLzA4j2BjNXTknh",
            "outputIndex": 0,
            "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
            "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
            "input": {
              "amount": 2352999000000,
              "signatureIndices": [0]
            }
          }
        ],
        "memo": "0x"
      },
      "credentials": [
        {
          "fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
          "credential": {
            "signatures": [
              "0xbeb83d3d29f1247efb4a3a1141ab5c966f46f946f9c943b9bc19f858bd416d10060c23d5d9c7db3a0da23446b97cd9cf9f8e61df98e1b1692d764c84a686f5f801"
            ]
          }
        }
      ]
    },
    "encoding": "json"
  },
  "id": 1
}
```
