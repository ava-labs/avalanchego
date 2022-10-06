#!/usr/bin/env bash

# Set up the versions to be used
subnet_evm_version=${SUBNET_EVM_VERSION:-'v0.4.0'}
# Don't export them as they're used in the context of other calls
avalanche_version=${AVALANCHE_VERSION:-'v1.9.0'}
network_runner_version=${NETWORK_RUNNER_VERSION:-'e7eb33c1e830e6be0a39162ebf4a3abee80a335b'}
ginkgo_version=${GINKGO_VERSION:-'v2.2.0'}

