# Cross Subnet Virtual Machine (XSVM)

Cross Subnet Asset Transfers README Overview

[Background](#avalanche-subnets-and-custom-vms)

[Introduction](#introduction)

[Usage](#how-it-works)

[Running](#running-the-vm)

[Demo](#cross-subnet-transaction-example)

## Avalanche Subnets and Custom VMs

Avalanche is a network composed of multiple sub-networks (called [subnets][Subnet]) that each contain any number of blockchains. Each blockchain is an instance of a [Virtual Machine (VM)](https://build.avax.network/docs/quick-start/virtual-machines), much like an object in an object-oriented language is an instance of a class. That is, the VM defines the behavior of the blockchain where it is instantiated. For example, [Coreth (EVM)][Coreth] is a VM that is instantiated by the [C-Chain]. Likewise, one could deploy another instance of the EVM as their own blockchain (to take this to its logical conclusion).

## Introduction

Just as [Coreth] powers the [C-Chain], XSVM can be used to power its own blockchain in an Avalanche [Subnet]. Instead of providing a place to execute Solidity smart contracts, however, XSVM enables asset transfers for assets originating on its own chain or other XSVM chains on other subnets.

## How it Works

XSVM utilizes AvalancheGo's [interchain messaging] package to create and authenticate Subnet Messages.

### Transfer

If you want to send an asset to someone, you can use a `tx.Transfer` to send to any address.

### Export

If you want to send this chain's native asset to a different subnet, you can use a `tx.Export` to send to any address on a destination chain. You may also use a `tx.Export` to return the destination chain's native asset.

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
  account     Displays the state of the requested account
  chain       Manages XS chains
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  issue       Issues transactions
  version     Prints out the version
  versionjson Prints out the version in json format

Flags:
  -h, --help   help for xsvm

Use "xsvm [command] --help" for more information about a command.
```

### [Golang SDK](https://github.com/ava-labs/avalanchego/blob/master/vms/example/xsvm/api/client.go)

```golang
// Client defines xsvm client operations.
type Client interface {
  Network(
    ctx context.Context,
    options ...rpc.Option,
  ) (uint32, ids.ID, ids.ID, error)
  Genesis(
    ctx context.Context,
    options ...rpc.Option,
  ) (*genesis.Genesis, error)
  Nonce(
    ctx context.Context,
    address ids.ShortID,
    options ...rpc.Option,
  ) (uint64, error)
  Balance(
    ctx context.Context,
    address ids.ShortID,
    assetID ids.ID,
    options ...rpc.Option,
  ) (uint64, error)
  Loan(
    ctx context.Context,
    chainID ids.ID,
    options ...rpc.Option,
  ) (uint64, error)
  IssueTx(
    ctx context.Context,
    tx *tx.Tx,
    options ...rpc.Option,
  ) (ids.ID, error)
  LastAccepted(
    ctx context.Context,
    options ...rpc.Option,
  ) (ids.ID, *block.Stateless, error)
  Block(
    ctx context.Context,
    blkID ids.ID,
    options ...rpc.Option,
   (*block.Stateless, error)
  Message(
    ctx context.Context,
    txID ids.ID,
    options ...rpc.Option,
  ) (*warp.UnsignedMessage, []byte, error)
}
```

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

Anyone can deploy their own instance of the XSVM as a subnet on Avalanche. All you need to do is compile it, create a genesis, and send a few txs to the
P-Chain.

You can do this by following the [subnet tutorial] or by using the [subnet-cli].

[interchain messaging]: https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/warp/README.md
[subnet tutorial]: https://build.avax.network/docs/tooling/create-avalanche-l1
[Coreth]: https://github.com/ava-labs/avalanchego/graft/coreth
[C-Chain]: https://build.avax.network/docs/quick-start/primary-network#c-chain
[Subnet]: https://build.avax.network/docs/avalanche-l1s

## Cross Subnet Transaction Example

The following example shows how to interact with the XSVM to send and receive native assets across subnets.

### Overview of Steps

1. Create & deploy Subnet A
2. Create  & deploy Subnet B
3. Issue an **export** Tx on Subnet A
4. Issue an **import** Tx on Subnet B
5. Confirm Txs processed correctly

> **Note:**  This demo requires [avalanche-cli](https://github.com/ava-labs/avalanche-cli) version > 1.0.5, [xsvm](https://github.com/ava-labs/xsvm) version > 1.0.2 and [avalanche-network-runner](https://github.com/ava-labs/avalanche-network-runner) v1.3.5.

### Create and Deploy Subnet A, Subnet B

Using the avalanche-cli, this step deploys two subnets running the XSVM. Subnet A will act as the sender in this demo, and Subnet B will act as the receiver.

Steps

Build the [XSVM](https://github.com/ava-labs/xsvm)

### Create a genesis file

```bash
xsvm chain genesis --encoding binary > xsvm.genesis
```

### Create Subnet A and Subnet B

```bash
avalanche subnet create subnetA --custom --genesis <path_to_genesis> --vm <path_to_vm_binary>
avalanche subnet create subnetB --custom --genesis <path_to_genesis> --vm <path_to_vm_binary>
```

### Deploy Subnet A and Subnet B

```bash
avalanche subnet deploy subnetA --local
avalanche subnet deploy subnetB --local
```

### Issue Export Tx from Subnet A

The SubnetID and ChainIDs are stored in the sidecar.json files in your avalanche-cli directory. Typically this is located at $HOME/.avalanche/subnets/

```bash
xsvm issue export --source-chain-id <SubnetA.BlockchainID> --amount <export_amount> --destination-chain-id <SubnetB.BlockchainID>
```

Save the TxID printed out by running the export command.

### Issue Import Tx from Subnet B

> Note: The import tx requires **snowman++** consensus to be activated on the importing chain. A chain requires ~3 blocks to be produced for snowman++ to start.
> Run `xsvm issue transfer --chain-id <SubnetB.BlockchainID> --amount 1000`  to issue simple Txs on SubnetB

```bash
xsvm issue import --source-chain-id <SubnetA.BlockchainID> --destination-chain-id <SubnetB.BlockchainID> --tx-id <exportTxID> --source-uris <source_uris>
```

> The <source_uris> can be found by running `avalanche network status`. The default URIs are
"<http://localhost:9650,http://localhost:9652,http://localhost:9654,http://localhost:9656,http://localhost:9658>"

**Account Values**
To check proper execution, use the `xsvm account` command to check balances.

Verify the balance on SubnetA decreased by your export amount using

```bash
xsvm account --chain-id <SubnetA.BlockchainID>
```

Now verify chain A's assets were successfully imported to SubnetB

```bash
xsvm account --chain-id <SubnetB.BlockchainID> --asset-id <SubnetA.BlockchainID>
```
