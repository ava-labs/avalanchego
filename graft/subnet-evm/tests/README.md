# Developing with tmpnet

The `load/` and `warp/` paths contain end-to-end (e2e) tests that use
the [tmpnet
fixture](https://github.com/ava-labs/avalanchego/blob/master/tests/fixture/tmpnet/README.md). By
default both test suites use the tmpnet fixture to create a temporary
network that exists for only the duration of their execution.

## Running the tests

Run the suites from the root of the avalanchego repository with these tasks:

```bash
task bazel-test-load-subnet-evm
task bazel-test-warp-subnet-evm
```

The tasks use Bazel to build AvalancheGo, the Subnet-EVM plugin, the test
binary, and (for the load suite) the load simulator. The load suite requires
the prebuilt simulator, so `ginkgo ./tests/load` does not work without these
tasks. For task options, see
[E2E and monitored test runners](../../../docs/bazel.md#e2e-and-monitored-test-runners).

## Reusing a temporary network

It is possible to create a temporary network that can be reused across
test runs to minimize the setup cost involved:

```bash
# From the root of a clone of avalanchego, build the tmpnetctl cli
$ ./scripts/build_tmpnetctl.sh

# Start a new temporary network configured with subnet-evm's default plugin path
$ ./build/tmpnetctl start-network --avalanche-path=./build/avalanchego

# From the root of a clone of subnet-evm, execute the warp test suite against the existing network
$ ginkgo -vv ./tests/warp -- --use-existing-network --network-dir=$HOME/.tmpnet/networks/latest

# To stop the temporary network when no longer needed, execute the following from the root of the clone of avalanchego
$ ./build/tmpnetctl stop-network --network-dir=$HOME/.tmpnet/networks/latest
```

The network started by `tmpnetctl` won't come with subnets configured,
so the test suite will add them to the network the first time it
runs. Subsequent test runs will be able to reuse those subnets without
having to set them up.

## Collection of logs and metrics

Logs and metrics can be optionally collected for tmpnet networks and
viewed in grafana. The details of configuration and usage for
subnet-evm mirror those of avalanchego and the same
[documentation](https://github.com/ava-labs/avalanchego/blob/master/tests/fixture/tmpnet/README.md#Monitoring)
applies.

## Optional Dev Shell

Some activities, such as collecting metrics and logs from the nodes targeted by an e2e
test run, require binary dependencies. One way of making these dependencies available is
to use a nix shell which will give access to the dependencies expected by the test
tooling:

- Install Nix. For installation and repo dev shell setup, see
  [CONTRIBUTING.md](../../../CONTRIBUTING.md#nix).
- Run `nix develop` to start a dev shell for the repo flake
- Execute the dependency-requiring command (e.g. `ginkgo -v ./tests/warp -- --start-collectors`)

This repo also defines a [`.envrc`](../../../.envrc). For repo-level `direnv` setup and behavior,
see [CONTRIBUTING.md](../../../CONTRIBUTING.md#direnv).
