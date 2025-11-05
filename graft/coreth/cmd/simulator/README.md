# Load Simulator

When building developing your own blockchain using `coreth`, you may want to analyze how your fee parameterization behaves and/or how many resources your VM uses under different load patterns. For this reason, we developed `cmd/simulator`. `cmd/simulator` lets you drive arbitrary load across any number of [endpoints] with a user-specified `keys` directory (insecure) `timeout`, `workers`, `max-fee-cap`, and `max-tip-cap`.

## Building the Load Simulator

To build the load simulator, navigate to the base of the simulator directory:

```bash
cd $GOPATH/src/github.com/ava-labs/avalanchego/graft/coreth/cmd/simulator
```

Build the simulator:

```bash
go build -o ./simulator main/*.go
```

To confirm that you built successfully, run the simulator and print the version:

```bash
./simulator --version
```

This should give the following output:

```
v0.1.0
```

To run the load simulator, you must first start an EVM based network. The load simulator works on both the C-Chain and Subnet-EVM, so we will start a single node network and run the load simulator on the C-Chain.

To start a single node network, follow the instructions from the AvalancheGo [README](https://github.com/ava-labs/avalanchego#building-avalanchego) to build from source.

Once you've built AvalancheGo, open the AvalancheGo directory in a separate terminal window and run a single node non-staking network with the following command:

```bash
./build/avalanchego --sybil-protection-enabled=false --network-id=local
```

WARNING:

The `--sybil-protection-enabled=false` flag is only suitable for local testing. Disabling staking serves two functions explicitly for testing purposes:

1. Ignore stake weight on the P-Chain and count each connected peer as having a stake weight of 1
2. Automatically opts in to validate every Subnet

Once you have AvalancheGo running locally, it will be running an HTTP Server on the default port `9650`. This means that the RPC Endpoint for the C-Chain will be <http://127.0.0.1:9650/ext/bc/C/rpc> and ws://127.0.0.1:9650/ext/bc/C/ws for WebSocket connections.

Now, we can run the simulator command to simulate some load on the local C-Chain for 30s:

```bash
./simulator --timeout=1m --workers=1 --max-fee-cap=300 --max-tip-cap=10 --txs-per-worker=50
```

## Command Line Flags

To see all of the command line flag options, run

```bash
./simulator --help
```
