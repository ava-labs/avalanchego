# Throughput testing

A throughput test is run in two parts. First a network must be running with at least one of the nodes running a throughput server. To start a throughput server when running a node the `--xput-server-enabled=true` flag should be passed.

An example single node network can be started with:

```sh
./build/ava --public-ip=127.0.0.1 --xput-server-port=9652 --xput-server-enabled=true --db-enabled=false --staking-tls-enabled=false --snow-sample-size=1 --snow-quorum-size=1
```

The thoughput node can be started with:

```sh
./build/xputtest --ip=127.0.0.1 --port=9652 --sp-chain
```

The above example with run a throughput test on the simple payment chain. Tests can be run with `--sp-dag` to run throughput tests on the simple payment dag. Tests can be run with `--avm` to run throughput tests on the AVA virtual machine.
