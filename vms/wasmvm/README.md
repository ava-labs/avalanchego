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

Now we can see this transaction's result by calling `wasm.getTx`:

```sh
curl --location --request POST 'localhost:9650/ext/bc/wasm' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "wasm.getTx",
    "params": {
        "id": "QSGQSXYjYkx4kZAhCPTqNcfJuoCJbMwx5oSFEbRhoWGcRT7YQ"
    },
    "id": 1
}'
```

The response indicates that the transaction was a contract invocation and that the method being called didn't return an error.
The response show the arguments to the invoked method as well as the returned value.
`byteArguments` and `returnValue` are both encoded with base 58 and a checksum.
In this case, both have value `"45PJLL"`, which is the encoding for an empty byte array.
That is, this method received no `byteArguments` and returned nothing (void.) 

```json
{
    "jsonrpc": "2.0",
    "result": {
        "tx": {
            "invocationSuccessful": true,
            "returnValue": "45PJLL",
            "status": "Accepted",
            "tx": {
                "arguments": [
                    1
                ],
                "byteArguments": "45PJLL",
                "contractID": "2JeuXkXVhk7QQEdJprU8DUgEZs4upMyCK82tjLaTCQcdwtmnVD",
                "function": "create_owner"
            },
            "type": "contract invocation"
        }
    },
    "id": 1
}
```

## Imported Functions

WASM smart contracts may (and in practice do) need to import information/functionality from the "outside world" (ie the WASM chain.)
The WASM chain provides an interface for the contracts to use.
Part of this interface is a key/value database.
Each contract has its database that only it reads/writes.

Right now, the following methods are provided to contracts:

* void print(int ptr, int len)
    * Print to the chain's log
    * `ptr` is a pointer to the first element of a byte array
    * `len` is the byte array's length  
* int dbPut(int key, int keyLen, int value, int valueLen)
    * Put a key/value pair in the smart contract's database
    * `key` is a pointer to the first element of a byte array (the key.)
    * `len` is the byte array's length  
    * Similar for `value` and `valueLen`
    * Returns 0 on success, otherwise non 0
* int dbGet(int key, int keyLen, int value)
    * Get a value from the smart contract's database.
    * `key` and `keyLen` specify the key.
    * `value` is a pointer to a buffer to write the value to.
    * Returns the length of the value, or -1 on failure.
* int dbGetValueLen(int keyPtr, int keyLen)
    * Return the length of the value whose key is specified by `keyPtr` and `keyLen`
    * Return -1 on failure

## Calling Conventions

WASM methods can only take as arguments integers and floats, and can only return an integer or a float.
This model is rather restrictive, so we've created a calling convention to allow WASM smart contract methods to handle more expressive arguments and return values.

### Byte Arguments

One can pass a byte array to a contract method by providing argument `byteArgs` when calling `wasm.Invoke`.

You may be thinking, "wait, didn't you just say WASM methods can't take byte array aruments?" Well, that's true. To get around that, at the start of every contract method invocation, we write `byteArgs` to the contract's database. Then, the contract can read `byteArgs` and use them however it likes. The convention is that `byteArgs` is mapped to in the contract's database by the empty key (that is, an empmty byte array.)

#### Example

The method `print_byte_args` in the contract we defined above reads the byte arguments to it, then uses `print` to print them.

When we call it:

```sh
{
    "jsonrpc": "2.0",
    "method": "wasm.invoke",
    "params": {
        "contractID": "2JeuXkXVhk7QQEdJprU8DUgEZs4upMyCK82tjLaTCQcdwtmnVD",
        "function": "print_byte_args",
        "args": [],
        "byteArgs":"U1Gavwb6Dr7nwea5Qgp2hPNv1fDg2o5XAzHpWtcEBS5cq6F78Nv5GUxp"
    },
    "id": 1
}
```

The following line is printed to the node's output:

```
Print from smart contract: {"fizz":{"buzz":["baz"]},"foo":"bar"}
```

That JSON is the `byteArgs` we passed in.

### Return Values

We also have a calling convention to allow returning complex values from a contract method.

The convention is that a method's literal return value (ie `return X`) is solely a success/failure indicator. If a method executes successfully, it returns 0. Otherwise, it returns some other integer. This determines the value of `invocationSuccessful` when calling `getTx`.

If the method wants to return a value, it converts it to a byte array and writes it to the database, where the key is the byte array containing only 1 (ie `[1]`) This determines the `returnValue` retrieved when calling `getTx`.

#### Example

Let's invoke contract method `get_num_bags`, which returns the number of bags that a given owner owns. This method takes one argument, the owner's ID.

```sh
{
    "jsonrpc": "2.0",
    "method": "wasm.invoke",
    "params": {
        "contractID": "2JeuXkXVhk7QQEdJprU8DUgEZs4upMyCK82tjLaTCQcdwtmnVD",
        "function": "get_num_bags",
        "args": [
            {
                "type": "int32",
                "value": 123
            }
        ]
    },
    "id": 1
}
```

This returns `txID` `8UMzqASLDGo1ehWrgfnN151zZZtZh3ZDrRt4njQNRTHEmNHZv`.

To look at the return value, we call `wasm.getTx`:

```sh
{
    "jsonrpc": "2.0",
    "method": "wasm.getTx",
    "params": {
        "id": "8UMzqASLDGo1ehWrgfnN151zZZtZh3ZDrRt4njQNRTHEmNHZv"
    },
    "id": 1
}
```

The `returnValue`, when decoded, is byte array `[0 0 0 0]`. Interpreted as a big-endian integer, this is 0. (As in, the owner owns 0 bags.) 

```sh
{
    "jsonrpc": "2.0",
    "result": {
        "tx": {
            "invocationSuccessful": true,
            "returnValue": "1111XiaYg",
            "status": "Accepted",
            "tx": {
                "arguments": [
                    1
                ],
                "byteArguments": "45PJLL",
                "contractID": "8UMzqASLDGo1ehWrgfnN151zZZtZh3ZDrRt4njQNRTHEmNHZv",
                "function": "get_num_bags"
            },
            "type": "contract invocation"
        }
    },
    "id": 1
}
```

## State Management

The contract's state is persisted after every method invocation, and loaded before every method invocation.

That means that global variables, etc. in the contract that are updated internally are persisted across calls.