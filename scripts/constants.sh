#!/usr/bin/env bash
#
# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# Where AvalancheGo binary goes
avalanchego_path="$AVALANCHE_PATH/build/avalanchego"
plugin_dir=${PLUGIN_DIR:-$HOME/.avalanchego/plugins}
evm_path=${EVM_PATH:-$plugin_dir/evm}
coreth_version=${CORETH_VERSION:-'v0.11.9-rc.0'}

# Set the PATHS
GOPATH="$(go env GOPATH)"
coreth_path=${CORETH_PATH:-"$GOPATH/pkg/mod/github.com/ava-labs/coreth@$coreth_version"}

# Avalabs docker hub
# avaplatform/avalanchego - defaults to local as to avoid unintentional pushes
# You should probably set it - export DOCKER_REPO='avaplatform/avalanchego'
avalanchego_dockerhub_repo=${DOCKER_REPO:-"avalanchego"}

# Current branch
# TODO: fix "fatal: No names found, cannot describe anything" in github CI
current_branch=$(git symbolic-ref -q --short HEAD || git describe --tags --exact-match || true)

git_commit=${AVALANCHEGO_COMMIT:-$( git rev-list -1 HEAD )}

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
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"
# While CGO_ENABLED doesn't need to be explicitly set, it produces a much more
# clear error due to the default value change in go1.20.
export CGO_ENABLED=1
