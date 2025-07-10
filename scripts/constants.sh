#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# Where AvalancheGo binary goes
avalanchego_path="$AVALANCHE_PATH/build/avalanchego"

# Static compilation
static_ld_flags=''
if [ "${STATIC_COMPILATION:-}" = 1 ]
then
    export CC=musl-gcc
    which $CC > /dev/null || ( echo $CC must be available for static compilation && exit 1 )
    static_ld_flags=' -extldflags "-static" -linkmode external '
fi

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"
export CGO_ENABLED=1 # Required for cross-compilation

# Disable version control fallbacks
export GOPROXY="${GOPROXY:-https://proxy.golang.org}"
