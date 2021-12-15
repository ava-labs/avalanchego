#!/usr/bin/env bash

# Set up the versions to be used
subnet_evm_version=${SUBNET_EVM_VERSION:-'v0.1.0'}
# Don't export them as they're used in the context of other calls
avalanche_version=${AVALANCHE_VERSION:-'v1.6.6-rc.0'}
