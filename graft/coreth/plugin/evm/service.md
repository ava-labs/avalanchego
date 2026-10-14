---
title: C-Chain API
description: "This page is an overview of the C-Chain API associated with AvalancheGo."
---

> **Note:** Ethereum has its own notion of `networkID` and `chainID`. These have no relationship to Avalanche's view of networkID and chainID and are purely internal to the [C-Chain](https://build.avax.network/docs/quick-start/primary-network#c-chain). On Mainnet, the C-Chain uses `1` and `43114` for these values. On the Fuji Testnet, it uses `1` and `43113` for these values. `networkID` and `chainID` can also be obtained using the `net_version` and `eth_chainId` methods.

## Ethereum APIs

### Endpoints

#### JSON-RPC Endpoints

To interact with C-Chain via the JSON-RPC endpoint:

```sh
/ext/bc/C/rpc
```

To interact with other instances of the EVM via the JSON-RPC endpoint:

```sh
/ext/bc/blockchainID/rpc
```

where `blockchainID` is the ID of the blockchain running the EVM.

#### WebSocket Endpoints

> **Info:** The [public API node](https://build.avax.network/integrations#RPC%20Providers) (api.avax.network) supports HTTP APIs for X-Chain, P-Chain, and C-Chain, but websocket connections are only available for C-Chain. Other EVM chains are not available via websocket on the public API node.

To interact with C-Chain via the websocket endpoint:

```sh
/ext/bc/C/ws
```

For example, to interact with the C-Chain's Ethereum APIs via websocket on localhost, you can use:

```sh
ws://127.0.0.1:9650/ext/bc/C/ws
```

> **Tip:** On localhost, use `ws://`. When using the [Public API](https://build.avax.network/integrations#RPC%20Providers) or another host that supports encryption, use `wss://`.

To interact with other instances of the EVM via the websocket endpoint:

```sh
/ext/bc/blockchainID/ws
```

where `blockchainID` is the ID of the blockchain running the EVM.

### Standard Ethereum APIs

Avalanche offers an API interface identical to Geth's API except that it only supports the following
services:

- `web3_`
- `net_`
- `eth_`
- `personal_`
- `txpool_`
- `debug_` (note: this is turned off on the public API node.)

You can interact with these services the same exact way you'd interact with Geth (see exceptions below). See the
[Ethereum Wiki's JSON-RPC Documentation](https://ethereum.org/en/developers/docs/apis/json-rpc/)
and [Geth's JSON-RPC Documentation](https://geth.ethereum.org/docs/rpc/server)
for a full description of this API.

> **Info:** For batched requests on the [public API node](https://build.avax.network/integrations#RPC%20Providers) , the maximum number of items is 40.

#### Exceptions

Starting with release [`v0.12.2`](https://github.com/ava-labs/avalanchego/releases/tag/v1.12.2), `eth_getProof` has a different behavior compared to geth:

- On archival nodes (nodes with`pruning-enabled` set to `false`), queries for state proofs older than 24 hours preceding the last accepted block will be rejected by default. This can be adjusted with `historical-proof-query-window`, which defines the number of blocks before the last accepted block that can be queried for state proofs. Set this option to `0` to accept a state query for any block number.
- On pruning nodes (nodes with `pruning-enabled` set to `true`), queries for state proofs outside the 32 block window after the last accepted block are always rejected.

### Avalanche - Ethereum APIs

In addition to the standard Ethereum APIs, Avalanche offers `eth_baseFee`,
`eth_getChainConfig`, `eth_callDetailed`,
`eth_getBadBlocks`, `eth_suggestPriceOptions`,
and the `eth_newAcceptedTransactions` subscription.

They use the same endpoint as standard Ethereum APIs:

```sh
/ext/bc/C/rpc
```

#### `eth_baseFee`

Get the base fee for the next block.

**Signature:**

```sh
eth_baseFee() -> {}
```

`result` is the hex value of the base fee for the next block.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_baseFee",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x34630b8a00"
}
```

#### `eth_getChainConfig`

Returns the chain configuration.

**Signature:**

```sh
eth_getChainConfig() -> {}
```

`result` is the chain configuration object.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_getChainConfig",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "apricotPhase1BlockTimestamp": 1607144400,
        "apricotPhase2BlockTimestamp": 1607144400,
        "apricotPhase3BlockTimestamp": 1607144400,
        "apricotPhase4BlockTimestamp": 1607144400,
        "apricotPhase5BlockTimestamp": 1607144400,
        "apricotPhase6BlockTimestamp": 1607144400,
        "apricotPhasePost6BlockTimestamp": 1607144400,
        "apricotPhasePre6BlockTimestamp": 1607144400,
        "banffBlockTimestamp": 1607144400,
        "berlinBlock": 0,
        "byzantiumBlock": 0,
        "cancunTime": 1607144400,
        "chainId": 43112,
        "constantinopleBlock": 0,
        "cortinaBlockTimestamp": 1607144400,
        "daoForkBlock": 0,
        "daoForkSupport": true,
        "durangoBlockTimestamp": 1607144400,
        "eip150Block": 0,
        "eip155Block": 0,
        "eip158Block": 0,
        "etnaTimestamp": 1607144400,
        "fortunaTimestamp": 1607144400,
        "graniteTimestamp": 253399622400,
        "homesteadBlock": 0,
        "istanbulBlock": 0,
        "londonBlock": 0,
        "muirGlacierBlock": 0,
        "petersburgBlock": 0,
        "shanghaiTime": 1607144400
    }
}
```

#### `eth_callDetailed`

Performs the same operation as `eth_call`, but returns additional execution details including gas
used, any EVM error code, and return data.

**Signature:**

```sh
eth_callDetailed({
    from: address (optional),
    to: address,
    gas: quantity (optional),
    gasPrice: quantity (optional),
    maxFeePerGas: quantity (optional),
    maxPriorityFeePerGas: quantity (optional),
    value: quantity (optional),
    data: data (optional)
}, blockNumberOrHash, stateOverrides (optional)) -> {
    gas: quantity,
    errCode: number,
    err: string,
    returnData: data
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_callDetailed",
    "params" :[{
        "to": "0x...",
        "data": "0x..."
    }, "latest"]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "gas": 21000,
    "errCode": 0,
    "err": "",
    "returnData": "0x"
  }
}
```

#### `eth_getBadBlocks`

Returns a list of the last bad blocks that the client has seen on the network.

**Signature:**

```sh
eth_getBadBlocks() -> []{
    hash: hash,
    block: object,
    rlp: string,
    reason: object
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_getBadBlocks",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

#### `eth_suggestPriceOptions`

Returns suggested gas price options (slow, normal, fast) for the current network conditions. Each
option includes a `maxPriorityFeePerGas` and a `maxFeePerGas` value.

**Signature:**

```sh
eth_suggestPriceOptions() -> {
    slow: {
        maxPriorityFeePerGas: quantity,
        maxFeePerGas: quantity
    },
    normal: {
        maxPriorityFeePerGas: quantity,
        maxFeePerGas: quantity
    },
    fast: {
        maxPriorityFeePerGas: quantity,
        maxFeePerGas: quantity
    }
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_suggestPriceOptions",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "slow": {
      "maxPriorityFeePerGas": "0x5d21dba00",
      "maxFeePerGas": "0x6fc23ac00"
    },
    "normal": {
      "maxPriorityFeePerGas": "0x2540be400",
      "maxFeePerGas": "0x4a817c800"
    },
    "fast": {
      "maxPriorityFeePerGas": "0x12a05f200",
      "maxFeePerGas": "0x37e11d600"
    }
  }
}
```

#### `eth_newAcceptedTransactions` (Subscription)

Creates a subscription that fires each time a transaction is accepted (finalized) on the chain.
This is an Avalanche-specific subscription not available in standard Ethereum. If `fullTx` is
`true`, the full transaction object is sent; otherwise only the transaction hash is sent.

Available only via WebSocket.

**Signature:**

```sh
{"jsonrpc":"2.0", "id":1, "method":"eth_subscribe", "params":["newAcceptedTransactions", {"fullTx": false}]}
```

**Example Call:**

```sh
wscat -c ws://127.0.0.1:9650/ext/bc/C/ws -x '{"jsonrpc":"2.0", "id":1, "method":"eth_subscribe", "params":["newAcceptedTransactions"]}'
```

**Example Notification:**

```json
{
  "jsonrpc": "2.0",
  "method": "eth_subscription",
  "params": {
    "subscription": "0x...",
    "result": "0xtransactionhash..."
  }
}
```

## Warp APIs

The Warp API enables interaction with Avalanche Warp Messaging (AWM), allowing cross-chain
communication between Avalanche blockchains. It provides methods for retrieving warp messages
and their BLS signatures, as well as aggregating signatures from validators.

The Warp API is enabled when the `warp-api-enabled` flag is set to `true` in the node configuration.

### Warp API Endpoint

```sh
/ext/bc/C/rpc
```

### Warp API Methods

#### `warp_getMessage`

Returns the raw bytes of a warp message by its ID.

**Signature:**

```sh
warp_getMessage({messageID: string}) -> data
```

- `messageID` is the ID of the warp message.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"warp_getMessage",
    "params" :["2PsAgvhGczsTNxCMgVJG39gKV4W9ETXWL1mMJk3xoSRHMBcyY"]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x..."
}
```

#### `warp_getMessageSignature`

Returns BLS signature for the specified warp message.

**Signature:**

```sh
warp_getMessageSignature({messageID: string}) -> data
```

- `messageID` is the ID of the warp message.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"warp_getMessageSignature",
    "params" :["2PsAgvhGczsTNxCMgVJG39gKV4W9ETXWL1mMJk3xoSRHMBcyY"]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x..."
}
```

#### `warp_getBlockSignature`

Returns the BLS signature associated with a blockID.

**Signature:**

```sh
warp_getBlockSignature({blockID: string}) -> data
```

- `blockID` is the ID of the block in warp message.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"warp_getBlockSignature",
    "params" :["2PsAgvhGczsTNxCMgVJG39gKV4W9ETXWL1mMJk3xoSRHMBcyY"]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x..."
}
```

#### `warp_getMessageAggregateSignature`

Fetches BLS signatures from validators for the specified warp message and aggregates them into a
single signed warp message. The caller specifies the quorum numerator (the denominator is 100).

**Signature:**

```sh
warp_getMessageAggregateSignature({messageID: string, quorumNum: number, subnetID: string (optional)}) -> data
```

- `messageID` is the ID of the warp message.
- `quorumNum` is the quorum numerator (e.g., `67` for 67% quorum). The denominator is 100.
- `subnetID` (optional) is the subnet to aggregate signatures from. Defaults to the current subnet.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"warp_getMessageAggregateSignature",
    "params" :["2PsAgvhGczsTNxCMgVJG39gKV4W9ETXWL1mMJk3xoSRHMBcyY", 67, ""]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x..."
}
```

The returned bytes are the serialized signed warp message.

#### `warp_getBlockAggregateSignature`

Fetches BLS signatures from validators for the specified block and aggregates them into a single
signed warp message. The caller specifies the quorum numerator (the denominator is 100).

**Signature:**

```sh
warp_getBlockAggregateSignature({blockID: string, quorumNum: number, subnetID: string (optional)}) -> data
```

- `blockID` is the ID of the block.
- `quorumNum` is the quorum numerator (e.g., `67` for 67% quorum). The denominator is 100.
- `subnetID` (optional) is the subnet to aggregate signatures from. Defaults to the current subnet.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"warp_getBlockAggregateSignature",
    "params" :["2PsAgvhGczsTNxCMgVJG39gKV4W9ETXWL1mMJk3xoSRHMBcyY", 67, ""]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x..."
}
```

The returned bytes are the serialized signed warp message.

## Admin APIs

The Admin API provides administrative functionality for the EVM.

### Admin API Endpoint

```sh
/ext/bc/C/admin
```

### Admin API Methods

#### `admin_startCPUProfiler`

Starts a CPU profile that writes to the specified file.

**Signature:**

```sh
admin_startCPUProfiler() -> {}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin_startCPUProfiler",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

#### `admin_stopCPUProfiler`

Stops the CPU profile.

**Signature:**

```sh
admin_stopCPUProfiler() -> {}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin_stopCPUProfiler",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

#### `admin_memoryProfile`

Runs a memory profile writing to the specified file.

**Signature:**

```sh
admin_memoryProfile() -> {}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin_memoryProfile",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

#### `admin_lockProfile`

Runs a mutex profile writing to the specified file.

**Signature:**

```sh
admin_lockProfile() -> {}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin_lockProfile",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

#### `admin_setLogLevel`

Sets the log level for the EVM.

**Signature:**

```sh
admin_setLogLevel({
    level: string
}) -> {}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin_setLogLevel",
    "params" :[{
        "level": "debug"
    }]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

#### `admin_getVMConfig`

Returns the current VM configuration.

**Signature:**

```sh
admin_getVMConfig() -> {
    config: {
        // VM configuration fields
    }
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin_getVMConfig",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

## Avalanche-Specific APIs

### Avalanche-Specific API Endpoint

```sh
/ext/bc/C/avax
```

### Avalanche-Specific API Methods

#### `avax.getUTXOs`

Gets all UTXOs for the specified addresses.

**Signature:**

```sh
avax.getUTXOs({
    addresses: [string],
    sourceChain: string,
    startIndex: {
        address: string,
        utxo: string
    },
    limit: number,
    encoding: string
}) -> {
    utxos: [string],
    endIndex: {
        address: string,
        utxo: string
    },
    numFetched: string,
    encoding: string
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.getUTXOs",
    "params" :[{
        "addresses": ["X-avax1..."],
        "sourceChain": "X",
        "limit": 100,
        "encoding": "hex"
    }]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

#### `avax.issueTx`

Issues a transaction to the network.

**Signature:**

```sh
avax.issueTx({
    tx: string,
    encoding: string
}) -> {
    txID: string
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.issueTx",
    "params" :[{
        "tx": "0x...",
        "encoding": "hex"
    }]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

#### `avax.getAtomicTxStatus`

Returns the status of the specified atomic transaction.

**Signature:**

```sh
avax.getAtomicTxStatus({
    txID: string
}) -> {
    status: string,
    blockHeight: number (optional)
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.getAtomicTxStatus",
    "params" :[{
        "txID": "2QouvNW..."
    }]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

#### `avax.getAtomicTx`

Returns the specified atomic transaction.

**Signature:**

```sh
avax.getAtomicTx({
    txID: string,
    encoding: string
}) -> {
    tx: string,
    encoding: string,
    blockHeight: number (optional)
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.getAtomicTx",
    "params" :[{
        "txID": "2QouvNW...",
        "encoding": "hex"
    }]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```
