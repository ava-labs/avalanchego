#!/usr/bin/env bash

# Set up the versions to be used - populate ENV variables only if they are not already populated
SUBNET_EVM_VERSION=${SUBNET_EVM_VERSION:-'v0.5.4'}
# Don't export them as they're used in the context of other calls
AVALANCHE_VERSION=${AVALANCHE_VERSION:-'v1.10.6-rc.4'}
AVALANCHEGO_VERSION=${AVALANCHEGO_VERSION:-$AVALANCHE_VERSION}
GINKGO_VERSION=${GINKGO_VERSION:-'v2.2.0'}

# This won't be used, but it's here to make code syncs easier
LATEST_CORETH_VERSION='0.12.4-rc.0'
