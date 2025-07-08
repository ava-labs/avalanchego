---
title: C-Chain API
description: "This page is an overview of the C-Chain API associated with AvalancheGo."
---

<Callout title="Note">
Ethereum has its own notion of `networkID` and `chainID`. These have no relationship to Avalanche's view of networkID and chainID and are purely internal to the [C-Chain](/docs/quick-start/primary-network#c-chain). On Mainnet, the C-Chain uses `1` and `43114` for these values. On the Fuji Testnet, it uses `1` and `43113` for these values. `networkID` and `chainID` can also be obtained using the `net_version` and `eth_chainId` methods.
</Callout>

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

<Callout title="info">
On the [public API node](/docs/tooling/rpc-providers), it only supports C-Chain
websocket API calls for API methods that don't exist on the C-Chain's HTTP API
</Callout>

To interact with C-Chain via the websocket endpoint:

```sh
/ext/bc/C/ws
```

For example, to interact with the C-Chain's Ethereum APIs via websocket on localhost, you can use:

```sh
ws://127.0.0.1:9650/ext/bc/C/ws
```

<Callout title="Tip" icon = {<BadgeCheck className="size-5 text-card" fill="green" />} >
On localhost, use `ws://`. When using the [Public API](/docs/tooling/rpc-providers) or another
host that supports encryption, use `wss://`.
</Callout>

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

<Callout title="info">
For batched requests on the [public API node](/docs/tooling/rpc-providers) , the maximum
number of items is 40.
</Callout>

#### Exceptions

Starting with release [`v0.12.2`](https://github.com/ava-labs/avalanchego/releases/tag/v1.12.2), `eth_getProof` has a different behavior compared to geth:

- On archival nodes (nodes with`pruning-enabled` set to `false`), queries for state proofs older than 24 hours preceding the last accepted block will be rejected by default. This can be adjusted with `historical-proof-query-window`, which defines the number of blocks before the last accepted block that can be queried for state proofs. Set this option to `0` to accept a state query for any block number.
- On pruning nodes (nodes with `pruning-enabled` set to `true`), queries for state proofs outside the 32 block window after the last accepted block are always rejected.

### Avalanche - Ethereum APIs

In addition to the standard Ethereum APIs, Avalanche offers `eth_baseFee`,
`eth_maxPriorityFeePerGas`, and `eth_getChainConfig`.

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

#### `eth_maxPriorityFeePerGas`

Get the priority fee needed to be included in a block.

**Signature:**

```sh
eth_maxPriorityFeePerGas() -> {}
```

`result` is hex value of the priority fee needed to be included in a block.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_maxPriorityFeePerGas",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x2540be400"
}
```

For more information on dynamic fees see the [C-Chain section of the transaction fee
documentation](/docs/api-reference/guides/txn-fees#c-chain-fees).

## Admin APIs

The Admin API provides administrative functionality for the EVM.

### Endpoint

```sh
/ext/bc/C/admin
```

### Methods

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

### Endpoint

```sh
/ext/bc/C/avax
```

### Methods

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
    numFetched: number,
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

#### `avax.version`

Returns the version of the VM.

**Signature:**

```sh
avax.version() -> {
    version: string
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.version",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```
