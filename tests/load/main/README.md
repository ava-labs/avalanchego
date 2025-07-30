# EVM Load Test

This executable utilizes the `load` package to perform a load test against an instance of the C-Chain. 

## Prerequisites

Using this executable requires `nix`: to install `nix`, please refer to the AvalancheGo [flake.nix](../../../flake.nix) file for installation instructions.
Furthermore, utilization of any monitoring features requires credentials to the
Avalanche monitoring stack.

Reading the `load` package [README.md](../README.md) is highly recommended as well,
especially for those considering modifying this executable for their own load tests.

## Quick Start

To run an EVM load test, execute the following commands:

```bash
# Start nix development environment
nix develop

# Start load test with monitoring
task test-load -- --start-metrics-collector --start-logs-collector
```

This command will create a temporary Avalanche network and perform any test setup prior
to starting the load test.

### Monitoring

The test will start a new Avalanche network along with deploying a set of 
wallets which will send transactions to the network for the lifetime of the test.
To enable viewing the state of the test network, `tmpnet` will log a Grafana URL:

```
[07-25|13:47:36.137] INFO tmpnet/network.go:410 metrics and logs available via grafana (collectors must be running) {"url": "https://grafana-poc.avax-dev.network/d/kBQpRdWnk/avalanche-main-dashboard?&var-filter=network_uuid%7C%3D%7Ce1b9dd69-5204-4c24-8b98-d3aea14c0eeb&var-filter=is_ephemeral_node%7C%3D%7Cfalse&from=1753465644564&to=now"}
```

Clicking on this link will open the main AvalancheGo dashboard in Grafana
filtered to display only results from the test.

## Architecture

```mermaid
flowchart BT
  subgraph MetricsServer[Metrics Server]
    Metrics
  end

  subgraph Generator
    subgraph TestA[Test A]
      WalletA[Wallet A]
    end
    subgraph TestB[Test B]
      WalletB[Wallet B]
    end
    subgraph TestC[Test C]
      WalletC[Wallet C]
    end
  end

  subgraph Network
    NodeA
    NodeB
    NodeC
  end

  Generator --> Metrics

  WalletA --> NodeA
  WalletB --> NodeB
  WalletC --> NodeC
```

The load test architecture consists of two main components that work together to simulate realistic blockchain usage and a metrics server which exposes client-side metrics.

### Network

The network is created via `tmpnet`, which provisions a temporary cluster of validator nodes. The number of nodes is configurable through the `--node-count` flag (default: 5).
In addition to network creation, the executable directs `tmpnet` to create a prefunded account for each worker.

#### Kubernetes Support

By default, the nodes of a network created by a load test are local processes. However, it's possible for network nodes to run within a Kubernetes cluster as pods. For example, to run a load test with nodes in a [Kind](https://kind.sigs.k8s.io/) cluster, execute the following:

```bash
# Start nix development environment
nix develop

# Start load test against Kind cluster
task test-load-kube-kind
```

`nix` handles the installation of any Kubernetes/Kind dependencies, making it trivial to run load tests with a Kind cluster.

### Load Generator

The load generator is setup as follows:

1. Deploy an instance of the `EVMLoadSimulator` contract 
2. Create an instance of `RandomTest` which uses the deployed contract
3. Create an instance of `LoadGenerator` with a worker per node
4. Start the generator

### Metrics Server

For client-side metrics to be collected by `tmpnet` and uploaded to the Avalanche
monitoring stack, this executable also starts a metric server which exports the registry metrics
updated by the generator/wallets. The server is configured to be targeted by
`tmpnet`'s metrics collector via a service discovery config.

## Configuration

### Load Test Flags

- `--load-timeout`: Maximum duration to run the load test (default: unlimited)

### Network Configuration (`tmpnet` Flags)

The following common flags control the underlying Avalanche network setup:

- `--node-count`: Number of validator nodes in the test network (default: 5)
- `--start-metrics-collector`: Starts a metrics collector for node and test metrics. If already running, this is a no-op.
- `--start-logs-collector`: Starts a logs collector for node output. If already running, this is a no-op.

