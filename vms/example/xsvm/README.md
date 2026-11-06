# Cross Subnet Virtual Machine (XSVM)

Cross-L1 Asset Transfers README Overview

[Background](#avalanche-l1s-and-custom-vms)

[Introduction](#introduction)

[Usage](#how-it-works)

[Running](#running-the-vm)

[Demo](#cross-l1-transaction-example)

## Avalanche L1s and Custom VMs

Avalanche is a network composed of the Primary Network and any number of [L1s][L1], each of which runs its own blockchains. Each blockchain is an instance of a [Virtual Machine (VM)](https://build.avax.network/docs/quick-start/virtual-machines), much like an object in an object-oriented language is an instance of a class. That is, the VM defines the behavior of the blockchain where it is instantiated. For example, the [EVM] is a VM that is instantiated by the [C-Chain]. Likewise, one could deploy another instance of the EVM as their own blockchain (to take this to its logical conclusion).

## Introduction

Just as the [EVM] powers the [C-Chain], XSVM can be used to power its own blockchain as an Avalanche [L1]. Instead of providing a place to execute Solidity smart contracts, however, XSVM enables asset transfers for assets originating on its own chain or other XSVM chains on other L1s.

## How it Works

XSVM utilizes AvalancheGo's [interchain messaging] package to create and authenticate Warp messages.

### Transfer

If you want to send an asset to someone, you can use a `tx.Transfer` to send to any address.

### Export

If you want to send this chain's native asset to a different L1, you can use a `tx.Export` to send to any address on a destination chain. You may also use a `tx.Export` to return the destination chain's native asset.

### Import

To receive assets from another chain's `tx.Export`, you must issue a `tx.Import`. Note that, similarly to a bridge, the security of the other chain's native asset is tied to the other chain. The security of all other assets on this chain are unrelated to the other chain.

### Fees

Currently there are no fees enforced in the XSVM.

### xsvm

#### Install

```bash
git clone https://github.com/ava-labs/avalanchego.git;
cd avalanchego;
go install -v ./vms/example/xsvm/cmd/xsvm;
```

#### Usage

```
Runs an XSVM plugin

Usage:
  xsvm [flags]
  xsvm [command]

Available Commands:
  account      Displays the state of the requested account
  chain        Manages XS chains
  completion   Generate the autocompletion script for the specified shell
  help         Help about any command
  issue        Issues transactions
  version      Prints out the version
  version-json Prints out the version in json format

Flags:
  -h, --help   help for xsvm

Use "xsvm [command] --help" for more information about a command.
```

### Golang SDK

The `Client` in [`api/client.go`](./api/client.go) has one method per endpoint below: `Network`, `Genesis`, `Nonce`, `Balance`, `Loan`, `IssueTx`, `LastAccepted`, `Block` and `Message`.

### Public Endpoints

#### xsvm.network

```
<<< POST
{
  "jsonrpc": "2.0",
  "method": "xsvm.network",
  "params":{},
  "id": 1
}
>>> {"networkID":<uint32>, "subnetID":<ID>, "chainID":<ID>}
```

For example:

```bash
curl --location --request POST 'http://34.235.54.228:9650/ext/bc/28iioW2fYMBnKv24VG5nw9ifY2PsFuwuhxhyzxZB5MmxDd3rnT' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc": "2.0",
    "method": "xsvm.network",
    "params":{},
    "id": 1
}'
```

> `{"jsonrpc":"2.0","result":{"networkID":1000000,"subnetID":"2gToFoYXURMQ6y4ZApFuRZN1HurGcDkwmtvkcMHNHcYarvsJN1","chainID":"28iioW2fYMBnKv24VG5nw9ifY2PsFuwuhxhyzxZB5MmxDd3rnT"},"id":1}`

#### xsvm.genesis

```
<<< POST
{
  "jsonrpc": "2.0",
  "method": "xsvm.genesis",
  "params":{},
  "id": 1
}
>>> {"genesis":<genesis file>}
```

#### xsvm.nonce

```
<<< POST
{
  "jsonrpc": "2.0",
  "method": "xsvm.nonce",
  "params":{
    "address":<cb58 encoded>
  },
  "id": 1
}
>>> {"nonce":<uint64>}
```

#### xsvm.balance

```
<<< POST
{
  "jsonrpc": "2.0",
  "method": "xsvm.balance",
  "params":{
    "address":<cb58 encoded>,
    "assetID":<cb58 encoded>
  },
  "id": 1
}
>>> {"balance":<uint64>}
```

#### xsvm.loan

```
<<< POST
{
  "jsonrpc": "2.0",
  "method": "xsvm.loan",
  "params":{
    "chainID":<cb58 encoded>
  },
  "id": 1
}
>>> {"amount":<uint64>}
```

#### xsvm.issueTx

```
<<< POST
{
  "jsonrpc": "2.0",
  "method": "xsvm.issueTx",
  "params":{
    "tx":<bytes>
  },
  "id": 1
}
>>> {"txID":<cb58 encoded>}
```

#### xsvm.lastAccepted

```
<<< POST
{
  "jsonrpc": "2.0",
  "method": "xsvm.lastAccepted",
  "params":{},
  "id": 1
}
>>> {"blockID":<cb58 encoded>, "block":<json>}
```

#### xsvm.block

```
<<< POST
{
  "jsonrpc": "2.0",
  "method": "xsvm.block",
  "params":{
    "blockID":<cb58 encoded>
  },
  "id": 1
}
>>> {"block":<json>}
```

#### xsvm.message

```
<<< POST
{
  "jsonrpc": "2.0",
  "method": "xsvm.message",
  "params":{
    "txID":<cb58 encoded>
  },
  "id": 1
}
>>> {"message":<json>, "signature":<bytes>}
```

## Running the VM

To build the VM, run `./scripts/build_xsvm.sh`.

### Deploying Your Own Network

Anyone can deploy their own instance of the XSVM as an L1 on Avalanche. All you need to do is compile it, create a genesis, and register the L1 on the P-Chain.

You can do this by following the [L1 tutorial].

[interchain messaging]: https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/warp/README.md
[L1 tutorial]: https://build.avax.network/docs/tooling/create-avalanche-l1
[EVM]: https://github.com/ava-labs/avalanchego/tree/master/vms/evm
[C-Chain]: https://build.avax.network/docs/quick-start/primary-network#c-chain
[L1]: https://build.avax.network/docs/avalanche-l1s

## Cross-L1 Transaction Example

The following example starts a local [tmpnet](../../../tests/fixture/tmpnet/README.md) network with two XSVM L1s and moves an asset from one to the other. The same flow runs automatically as the XSVM e2e test ([`tests/e2e/vms/xsvm.go`](../../../tests/e2e/vms/xsvm.go)):

```bash
./scripts/build.sh        # builds build/avalanchego
./scripts/build_xsvm.sh   # builds build/xsvm and registers it as a plugin
E2E_SERIAL=1 ./scripts/tests.e2e.sh --ginkgo.focus-file=xsvm.go
```

To run the steps by hand instead, start a network and issue the transactions yourself.

### Start the network

```bash
E2E_SERIAL=1 ./scripts/tests.e2e.sh --start-network
```

This starts a two-node network with two L1s, `xsvm-a` and `xsvm-b`, each running the XSVM, and leaves it running. Nodes listen on random local ports.

Collect the chain IDs, node URIs, and the pre-funded key from the network directory:

```bash
N=$(ls -dt ~/.tmpnet/networks/*-avalanchego-e2e | head -1)
A=$(jq -r '.Chains[0].ChainID' $N/subnets/xsvm-a.json)
B=$(jq -r '.Chains[0].ChainID' $N/subnets/xsvm-b.json)
K=$(jq -r '.Chains[0].PreFundedKey' $N/subnets/xsvm-a.json)
AN=$(jq -r '.ValidatorIDs[0]' $N/subnets/xsvm-a.json); AU=$(jq -r .uri $N/$AN/process.json)
BN=$(jq -r '.ValidatorIDs[0]' $N/subnets/xsvm-b.json); BU=$(jq -r .uri $N/$BN/process.json)
```

Each L1 is validated by a different node, so issue transactions against the node that validates the chain.

### Issue Export Tx from L1 A

The genesis funds a random key rather than the CLI's default key, so pass it with `--private-key`.

```bash
build/xsvm issue export --uri $AU --source-chain-id $A --destination-chain-id $B --amount 1000 --private-key $K
```

Save the TxID from the `issued tx <TxID>` line printed by the export command. The exported amount is credited to `--to`, which defaults to the XSVM CLI's default address.

### Issue Import Tx on L1 B

> Note: The import tx requires **snowman++** consensus to be activated on the importing chain. A chain requires ~3 blocks to be produced for snowman++ to start. The import hangs until enough blocks exist.

```bash
for i in 1 2 3; do build/xsvm issue transfer --uri $BU --chain-id $B --amount 1000 --private-key $K; done
build/xsvm issue import --uri $BU --source-chain-id $A --destination-chain-id $B --tx-id <exportTxID> --source-uris $AU
```

The import is signed by the default key, which matches the default `--to` of the export.

### Account Values

To check proper execution, use the `xsvm account` command to check balances. Chain A's asset should now be credited on L1 B:

```bash
build/xsvm account --uri $BU --chain-id $B --asset-id $A
```

### Stop the network

```bash
./scripts/run_tmpnetctl.sh stop-network --network-dir=$N
```
