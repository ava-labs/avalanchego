#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

# Don't export them as they're used in the context of other calls
AVALANCHE_VERSION=${AVALANCHE_VERSION:-'v1.12.0-config-pebble-sync'}
GINKGO_VERSION=${GINKGO_VERSION:-'v2.2.0'}
