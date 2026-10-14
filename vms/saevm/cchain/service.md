---
title: C-Chain API
description: "This page is an overview of the C-Chain API associated with AvalancheGo."
---

> **Note:** Ethereum's chain ID has no relationship to Avalanche's view of networkID and chainID and is purely internal to the [C-Chain](https://build.avax.network/docs/quick-start/primary-network#c-chain). On Mainnet, the C-Chain uses `43114`. On the Fuji Testnet, it uses `43113`. Both `eth_chainId` and `net_version` return this value.

## Ethereum APIs

### Endpoints

#### JSON-RPC Endpoints

To interact with C-Chain via the JSON-RPC endpoint:

```sh
/ext/bc/C/rpc
```

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

### Standard Ethereum APIs

Avalanche offers an API interface identical to Geth's API except that it only supports the methods
listed in [Available APIs](https://build.avax.network/docs/nodes/chain-configs/primary-network/c-chain#available-apis),
which also lists the methods a node serves by default. The `debug_` methods are turned off on the public API node.

You can interact with these services the same exact way you'd interact with Geth (see exceptions below). See the
[Ethereum Wiki's JSON-RPC Documentation](https://ethereum.org/en/developers/docs/apis/json-rpc/)
and [Geth's JSON-RPC Documentation](https://geth.ethereum.org/docs/rpc/server)
for a full description of this API.

> **Info:** For batched requests on the [public API node](https://build.avax.network/integrations#RPC%20Providers) , the maximum number of items is 40.

#### Exceptions

The C-Chain executes blocks after consensus accepts them, as described in [ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-continuous-execution). A block is final as soon as it is accepted, but its receipts and post-execution state exist only once it has executed. This changes the following:

- `latest` is the last executed block.
- `pending` is the last executed block. If `api-resolve-pending-to-last-executed` is `false`, it is the last accepted block instead, and methods that read state reject `pending`.
- `safe` and `finalized` are the last settled block, the most recent block whose execution results are included in an accepted block.
- Methods that read state or receipts at an accepted block that hasn't executed yet wait for it to execute.
- A block header's `baseFeePerGas` is an upper bound on the base fee charged when the block executes, and its `stateRoot` is the post-execution state root of the last settled block when it was built.

### Avalanche - Ethereum APIs

In addition to the standard Ethereum APIs, Avalanche offers `eth_baseFee`,
`eth_getChainConfig`, `eth_callDetailed`, `eth_suggestPriceOptions`,
and the `newAcceptedTransactions` subscription.

They use the same endpoint as standard Ethereum APIs:

```sh
/ext/bc/C/rpc
```

#### `eth_baseFee`

Get an upper bound on the base fee for the next block.

**Signature:**

```sh
eth_baseFee() -> {}
```

`result` is the hex value of the upper bound on the base fee for the next block.

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
        "graniteTimestamp": 1607144400,
        "heliconTimestamp": 1607144400,
        "homesteadBlock": 0,
        "iglooTimestamp": 253399622400,
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
    gas: number,
    errCode: number,
    err: string,
    returnData: data
}
```

`errCode` is `3` if the call reverted, and `0` otherwise.

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
      "maxPriorityFeePerGas": "0x389fd980",
      "maxFeePerGas": "0xafd56d80"
    },
    "normal": {
      "maxPriorityFeePerGas": "0x3b9aca00",
      "maxFeePerGas": "0xb2d05e00"
    },
    "fast": {
      "maxPriorityFeePerGas": "0x3e95ba80",
      "maxFeePerGas": "0xb5cb4e80"
    }
  }
}
```

#### `newAcceptedTransactions` (Subscription)

Creates a subscription that fires each time a transaction is accepted (finalized) on the chain, before it is executed.
This is an Avalanche-specific subscription not available in standard Ethereum. If `fullTx` is
`true`, the full transaction object is sent; otherwise only the transaction hash is sent.

Available only via WebSocket.

**Signature:**

```sh
{"jsonrpc":"2.0", "id":1, "method":"eth_subscribe", "params":["newAcceptedTransactions", false]}
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
        "addresses": ["C-avax1..."],
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

#### `avax.getAtomicTx`

Returns the specified atomic transaction. It returns an error until the transaction is accepted.

**Signature:**

```sh
avax.getAtomicTx({
    txID: string,
    encoding: string
}) -> {
    tx: string,
    encoding: string,
    blockHeight: string
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
