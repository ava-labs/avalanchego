---
title: C-Chain API
description: "This page is an overview of the C-Chain API associated with AvalancheGo."
---

> **Note:** Ethereum's chain ID has no relationship to Avalanche's view of networkID and chainID and is purely internal to the [C-Chain](https://build.avax.network/docs/quick-start/primary-network#c-chain-contract-chain). On Mainnet, the C-Chain uses `43114`. On the Fuji Testnet, it uses `43113`. Both `eth_chainId` and `net_version` return this value.

## Ethereum APIs

### Endpoints

The examples on this page call the public APIs at `api.avax.network`. To call your own node, replace `https://api.avax.network` with `http://127.0.0.1:9650`, and `wss://api.avax.network` with `ws://127.0.0.1:9650`.

#### JSON-RPC Endpoints

To interact with C-Chain via the JSON-RPC endpoint:

```sh
/ext/bc/C/rpc
```

#### WebSocket Endpoints

To interact with C-Chain via the WebSocket endpoint:

```sh
/ext/bc/C/ws
```

For example, the public APIs serve the C-Chain's Ethereum WebSocket endpoint at:

```sh
wss://api.avax.network/ext/bc/C/ws
```

> **Tip:** Use `ws://` for a node that serves HTTP. Use `wss://` for a node that serves HTTPS.

When node tracing is enabled (`--tracing-exporter-type`), JSON-RPC calls over HTTP are traced from the request span into the VM. Calls over the WebSocket endpoint are **not** traced: libevm's WebSocket server roots every call in a background context, so the request span never reaches the VM. Transactions submitted over WebSocket still appear in block-building and execution spans by `tx_hash`.

### Standard Ethereum APIs

The C-Chain serves methods from the [Ethereum JSON-RPC API](https://ethereum.org/en/developers/docs/apis/json-rpc/)
and [Geth's JSON-RPC API](https://geth.ethereum.org/docs/interacting-with-geth/rpc) over HTTP and WebSocket.
It doesn't serve IPC or GraphQL.

[Available APIs](https://build.avax.network/docs/nodes/chain-configs/primary-network/c-chain#available-apis)
lists every method that the C-Chain serves, grouped by API, and the APIs that are enabled by default.
The [`apis`](https://build.avax.network/docs/nodes/chain-configs/primary-network/c-chain#apis) option
selects the APIs that a node serves.

The node has no accounts, so `eth_resend`, `eth_sendTransaction`, `eth_sign`, and `eth_signTransaction` always fail,
and `eth_pendingTransactions` always returns an empty list.

#### Exceptions

The C-Chain executes blocks after consensus accepts them, as described in [ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-continuous-execution). This changes the following:

- `latest` and `pending` are the last executed block. If `api-resolve-pending-to-last-executed` is `false`, `pending` is the last accepted block, and state reads such as `eth_getBalance` and `eth_call` reject it. `eth_getTransactionCount` with `pending` always returns the transaction pool nonce.
- `safe` and `finalized` are the last settled block, the most recent block whose execution results are included in an accepted block.
- State and receipt reads wait for an accepted block to execute. `eth_getLogs` and log filters don't wait: for a block that hasn't executed, they return no logs, or an error if the query has no address or topic filter.
- From Helicon, block headers change:
  - `stateRoot` is the post-execution state root of the last block that the block settles, or its parent's `stateRoot` if it settles no blocks.
  - `receiptsRoot` and `logsBloom` cover the receipts of the blocks that it settles.
  - `gasUsed` is the sum of its transactions' gas limits, and `baseFeePerGas` is an upper bound on the base fee charged when it executes. Receipts report `gasUsed` and `effectiveGasPrice` from execution.

### Avalanche - Ethereum APIs

In addition to the standard Ethereum APIs, Avalanche offers `eth_baseFee`,
`eth_getChainConfig`, `eth_suggestPriceOptions`,
and the `newAcceptedTransactions` subscription.

They use the same endpoints as the standard Ethereum APIs.

#### `eth_baseFee`

Get an upper bound on the base fee for the next block.

**Signature:**

```sh
eth_baseFee() -> quantity
```

`result` is the hex value of the upper bound on the base fee for the next block.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_baseFee"
}' -H 'content-type:application/json;' https://api.avax.network/ext/bc/C/rpc
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

`result` is the chain configuration object. The public APIs don't serve this method, so this example calls a local node.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_getChainConfig"
}' -H 'content-type:application/json;' http://127.0.0.1:9650/ext/bc/C/rpc
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
    "method" :"eth_suggestPriceOptions"
}' -H 'content-type:application/json;' https://api.avax.network/ext/bc/C/rpc
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

Creates a subscription that fires each time a transaction is accepted on the chain, before it is executed.
This is an Avalanche-specific subscription not available in standard Ethereum. If `fullTx` is
`true`, the full transaction object is sent; otherwise only the transaction hash is sent.

Available only via WebSocket.

**Signature:**

```sh
{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_subscribe",
    "params" :["newAcceptedTransactions", false]
}
```

**Example Call:**

```sh
wscat -w -1 -c wss://api.avax.network/ext/bc/C/ws -x '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_subscribe",
    "params" :["newAcceptedTransactions"]
}'
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

Gets the UTXOs that `sourceChain` exported to the C-Chain for the specified addresses and that no executed block has imported yet. To get the next page, set `startIndex` to the returned `endIndex`. Stop when `utxos` is empty.

**Signature:**

```sh
avax.getUTXOs({
    addresses: [string],
    sourceChain: string,
    startIndex: {
        address: string,
        utxo: string
    } (optional),
    limit: number (optional),
    encoding: string (optional)
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
    "params" :{
        "addresses": ["C-avax1m03jar35fw6q9rk63wqkl5j925xqwxhct8g5w6"],
        "sourceChain": "X",
        "limit": 100,
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' https://api.avax.network/ext/bc/C/avax
```

#### `avax.issueTx`

Issues a transaction to the network.

**Signature:**

```sh
avax.issueTx({
    tx: string,
    encoding: string (optional)
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
    "params" :{
        "tx": "0x...",
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' https://api.avax.network/ext/bc/C/avax
```

#### `avax.getAtomicTx`

Returns the specified atomic transaction. It returns an error until the block that contains the transaction is executed.

**Signature:**

```sh
avax.getAtomicTx({
    txID: string,
    encoding: string (optional)
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
    "params" :{
        "txID": "2rgCo8mupzwgMfpVM7BDAgmteSDU6qrmQnJNtb37tscnN4fSwC",
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' https://api.avax.network/ext/bc/C/avax
```
