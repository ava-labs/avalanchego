# Load Simulator

When building developing your own blockchain using `subnet-evm`, you may want to analyze how your fee parameterization behaves and/or how many resources your VM uses under different load patterns. For this reason, we developed `cmd/simulator`. `cmd/simulator` lets you drive arbitrary load across any number of [endpoints] with a user-specified `keys` directory (insecure) `timeout`, `concurrency`, `base-fee`, and `priority-fee`.

## Building the Load Simulator

To build the load simulator, navigate to the base of the simulator directory:

```bash
cd $GOPATH/src/github.com/ava-labs/subnet-evm/cmd/simulator
```

Build the simulator:

```bash
go build -o ./simulator *.go
```

To confirm that you built successfully, run the simulator and print the version:

```bash
./simulator -v
```

This should give the following output:

```
v0.0.1
```

To run the load simulator, you must first start an EVM based network. The load simulator works on both the C-Chain and Subnet-EVM, so we will start a single node network and run the load simulator on the C-Chain.

To start a single node network, follow the instructions from the AvalancheGo [README](https://github.com/ava-labs/avalanchego#building-avalanchego) to build from source.

Once you've built AvalancheGo, open the AvalancheGo directory in a separate terminal window and run a single node non-staking network with the following command:

```bash
./build/avalanchego --staking-enabled=false --network-id=local
```

:::warning
The staking-enabled flag is only for local testing. Disabling staking serves two functions explicitly for testing purposes:

1. Ignore stake weight on the P-Chain and count each connected peer as having a stake weight of 1
2. Automatically opts in to validate every Subnet
:::

Once you have AvalancheGo running locally, it will be running an HTTP Server on the default port `9650`. This means that the RPC Endpoint for the C-Chain will be http://127.0.0.1:9650/ext/bc/C/rpc.

Now, we can run the simulator command to simulate some load on the local C-Chain for 30s:

```bash
RPC_ENDPOINTS=http://127.0.0.1:9650/ext/bc/C/rpc
./simulator --rpc-endpoints=$RPC_ENDPOINTS --keys=./.simulator/keys --timeout=30s --concurrency=10 --base-fee=300 --priority-fee=100
```

## Command Line Flags

### `rpc-endpoints` (string)

`rpc-endpoints` is a comma separated list of RPC endpoints to hit during the load test.

### `keys` (string)

`keys` specifies the directory to find the private keys to use throughout the test. The directory should contain files with the hex address as the name and the corresponding private key as the only content.

If the test needs to generate more keys (to meet the number of workers specified by `concurrency`), it will save them in this directory to ensure that the private keys holding funds at the end of the test are preserved.

:::warning
The `keys` directory is not a secure form of storage for private keys. This should only be used in local network and short lived network testing where losing or compromising the keys is not an issue.
:::

Note: if none of the keys in this directory have any funds, then the simulator will log an address that it expects to receive funds in order to fund the load test and wait for those funds to arrive.

### `timeout` (duration)

`timeout` specifies the duration to simulate load on the network for this test.

### `concurrency` (int)

`concurrency` specifies the number of concurrent workers that should send transactions to the network throughout the test. Each worker in the load test is a pairing of a private key and an RPC Endpoint. The private key is used to generate a stream of transactions, which are issued to the RPC Endpoint.

### `base-fee` (int)

`base-fee` specifies the base fee (denominated in GWei) to use for every transaction generated during the load test (generates Dynamic Fee Transactions).

### `priority-fee` (int)

`priority-fee` specifies the priority fee (denominated in GWei) to use for every transaction generated during the load test (generates Dynamic Fee Transactions).
