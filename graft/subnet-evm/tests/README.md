# Developing with tmpnet

The `load/` and `warp/` paths contain end-to-end (e2e) tests that use
the [tmpnet
fixture](https://github.com/ava-labs/avalanchego/blob/master/tests/fixture/tmpnet/README.md). By
default both test suites use the tmpnet fixture to create a temporary
network that exists for only the duration of their execution.

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

- Install [nix](https://nixos.org/). The [determinate systems installer](https://github.com/DeterminateSystems/nix-installer?tab=readme-ov-file#install-nix) is recommended.
- Use ./scripts/dev_shell.sh to start a nix shell
- Execute the dependency-requiring command (e.g. `ginkgo -v ./tests/warp -- --start-collectors`)

This repo also defines a `.envrc` file to configure [devenv](https://direnv.net/). With `devenv`
and `nix` installed, a shell at the root of the repo will automatically start a nix dev
shell.
