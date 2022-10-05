#!/usr/bin/env bash

# Set up the versions to be used
subnet_evm_version=${SUBNET_EVM_VERSION:-'v0.3.0'}
# Don't export them as they're used in the context of other calls
avalanche_version=${AVALANCHE_VERSION:-'v1.8.6'}
network_runner_version=${NETWORK_RUNNER_VERSION:-'v1.2.2'}
ginkgo_version=${GINKGO_VERSION:-'v2.1.4'}

