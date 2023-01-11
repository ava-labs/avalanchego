#!/usr/bin/env bash

# Set up the versions to be used
subnet_evm_version=${SUBNET_EVM_VERSION:-'v0.4.8'}
# Don't export them as they're used in the context of other calls
avalanche_version=${AVALANCHE_VERSION:-'v1.9.6'}
network_runner_version=${NETWORK_RUNNER_VERSION:-'v1.3.5'}
ginkgo_version=${GINKGO_VERSION:-'v2.2.0'}

# This won't be used, but it's here to make code syncs easier
latest_coreth_version=0.11.3
