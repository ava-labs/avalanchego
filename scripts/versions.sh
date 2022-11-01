#!/usr/bin/env bash

# Set up the versions to be used
subnet_evm_version=${SUBNET_EVM_VERSION:-'v0.4.2'}
# Don't export them as they're used in the context of other calls
avalanche_version=${AVALANCHE_VERSION:-'v1.9.1'}
network_runner_version=${NETWORK_RUNNER_VERSION:-'bb4b661ac88ebe50ab719424eecc1a55e01e7019'}
ginkgo_version=${GINKGO_VERSION:-'v2.2.0'}

