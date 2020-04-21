# WASM Smart Contract VM

This Virtual Machine implements a chain (the "WASM chain") that acts as a platform for WASM smart contracts.

It allows users to upload and invoke smart contracts.

In this branch, the WASM chain is included in the network genesis and is validated by the Default Subnet.
That is, it's "built-in"; you need not launch a subnet or the chain.

To launch a one node network:

``` sh
./build/ava --snow-sample-size=1 --snow-quorum-size=1 --network-id=local --staking-tls-enabled=false --public-ip=127.0.0.1 --log-level=debug
```

## A Sample Smart Contract

See [here](https://github.com/ava-labs/gecko-internal/blob/wasm/vms/wasmvm/contracts/rust_bag/src/lib.rs) for a Rust implementation of a smart contract.

In this smart contract there are 2 entities: bags and owners.

A bag has a handful of properties, such as price.
Each bag has an owner, and each owner has 0 or more bags.

The smart contract allows for the creation of owners and bags, the transfer of bags between owners, and the update of bag prices.

To compile the Rust code to WASM, we use the [wasm-pack](https://rustwasm.github.io/wasm-pack/installer/) tool in the Rust code's directory:

```sh
wasm-pack build
```

## Uploading a Smart Contract

To upload a smart contract, call `wasm.createContract`.
This API method takes one argument, `contract`, which is the base-58 with checksum representation of a WASM file.
Below, we create an instance of the contract we defined above.
Since the smart contract is 4 Kilobytes, we omit it below so as to not take up the whole page.

Sample call:

```sh
curl --location --request POST 'localhost:9650/ext/bc/wasm' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "wasm.createContract",
    "params": {
        "contract": "CONTRACT BYTES GO HERE"
    },
    "id": 1
}'
```

This method returns the ID of the generated transaction, which is also the ID of the smart contract:

```json
{
    "jsonrpc": "2.0",
    "result": "2JeuXkXVhk7QQEdJprU8DUgEZs4upMyCK82tjLaTCQcdwtmnVD",
    "id": 1
}
```

## Getting Transaction Details

We can get details about a transaction by calling `wasm.getTx`.
This API method takes one argument, the transaction's ID.

```sh
curl --location --request POST 'localhost:9650/ext/bc/wasm' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "wasm.getTx",
    "params": {
        "id": "2JeuXkXVhk7QQEdJprU8DUgEZs4upMyCK82tjLaTCQcdwtmnVD"
    },
    "id": 1
}'
```

For a contract creation transaction, this method returns the transaction's status, type and 
the transaction itself.

```json
{
    "jsonrpc": "2.0",
    "result": {
        "tx": {
            "status": "Accepted",
            "tx": {
                "contract": "13BMCShJG9LHijX3gSQPJGp5uz7qxpqcSicAiykz96WX2TsLE5jtQDwykTMJmQPg9BmjH81JxV88EpfNYjq8K74eheP2ZsU16NcUN7ufowMTrCPzxRcfR8h6C2K3bs4hKzWYPJ2pdR9LiWXgzrT1GcnYojwMzZzJwFvdqirw5JbsmYNkHAuSKp3HuUnsMTLbpYeJEk7NT9U8pNxCtrQrnMTeSWbmNoQAq8qmkgLF8sfVz6qfbzMLQgiwNfKU1yyqEXwi1PcH1uFNRs9r7tMnn8cRHqJPaefaHGp5tJocyEZAFR4ey5xDB24oibjh2d8h5QLg7df8wwEzhzuAE5967bhr8QrUazy1sLQ8zkfkqW15dnP2jNapPADm3ZZijR2Uq4vadUA8LiyB412qWdDi9jWRDmwnBCrWJePmABhURQpS5cWsjP9JgpkGqZicu8jGw425jLs2S6wMLsfkDAvhjdw6RjsdDsXFQ5TY4xb67G21wAFZkLgV7KeaPEDZoHgipqqURjykaq1nzXJEVWKKZGCKguam6HeEKSdBBSVJ8x4bm6xStWY5LnzWf8RHMp99GqT2pU9QgPkTDk3Jo8yWo7pKvnRCisFKup8jXomwz4ubeXKY4zwpjCunoZcjm3NW8yJSkiKf47iFAXGLtSmjz3q9zd7XGQBnVDpLvp5ZrRapCWxGqTAzpt4iePDiNxFKqi4NCnK19utStjz8TF5RMcVHze38z3cyDqKVPhJMu1HdTEvST5gLNCUE1qDZ6ynnamq1DEDDafYawNb1D5N35iedEeNpuEodqyKyrnsrcvVe4cPzHC8RHKZ64a1j2CkBZ6ruDvyJGXCC4uAgHzKtkoqoEaWQbPmr9EuZrb9rLpuu2rkNRpkEyr5Po8qz5Go4cu2iZrjLR6xYQ5ebE3FSYjtQS8Xbed25112MwUDQVYcMJEM8R7nyHYR2XWHawjUdL1bnbJgQk9WkxaNR7BNBVt2sLA5KCuAZrLC1uMDQkPss8gDqc5CppnsRsRC4F3NEtzFG1qSkaDuUvmGiJdaAUC4iPXnGgyL39kAaaTcgLmX8999gT6MLnRzBQBf3VL4oR9",
                "id": "2JeuXkXVhk7QQEdJprU8DUgEZs4upMyCK82tjLaTCQcdwtmnVD"
            },
            "type": "contract creation"
        }
    },
    "id": 1
}
```

## Invoking a Smart Contract

A smart contract that has been uploaded can be invoked by calling `wasm.invoke`.
This method takes four arguments:

* `contractID`: The contract being invoked
* `function`: The name of the function being invoked
* `args`: An array of integer arguments to pass to the function being invoked. May be omitted.
  * Each element of `args` must specify its type, which is one of: `int32`, `int64`
* `byteArgs`: The base 58 with checksum representation of a byte array to pass to the smart contract. May be omitted.

Let's invoke the contract's `create_owner` method.
As you can see in the Rust code, this method takes one arguments: the owner's ID.
We give the owner ID 123 below.

```sh
curl --location --request POST 'localhost:9650/ext/bc/wasm' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "wasm.invoke",
    "params":{
    	"contractID":"2JeuXkXVhk7QQEdJprU8DUgEZs4upMyCK82tjLaTCQcdwtmnVD",
    	"function":"create_owner",
        "args": [
            {
                "type": "int32",
                "value": 123
            }
        ]
    },
    "id": 1
}'
```

The resulting transaction's ID is returned:

```json
{
    "jsonrpc": "2.0",
    "result": {
        "txID": "QSGQSXYjYkx4kZAhCPTqNcfJuoCJbMwx5oSFEbRhoWGcRT7YQ"
    },
    "id": 1
}
```