#!/usr/bin/env bash
#
# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides
# Use the versions.sh to specify versions
#

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# Set the PATHS
GOPATH="$(go env GOPATH)"
coreth_path="$GOPATH/pkg/mod/github.com/ava-labs/coreth@$coreth_version"

# Where AvalancheGo binary goes
build_dir="$AVALANCHE_PATH/build"
avalanchego_path="$build_dir/avalanchego"
plugin_dir="$build_dir/plugins"
evm_path="$plugin_dir/evm"

# Avalabs docker hub
# avaplatform/avalanchego - defaults to local as to avoid unintentional pushes
# You should probably set it - export DOCKER_REPO='avaplatform/avalanchego'
avalanchego_dockerhub_repo=${DOCKER_REPO:-"avalanchego"}

# Current branch
current_branch=$(git symbolic-ref -q --short HEAD || git describe --tags --exact-match)

git_commit=${AVALANCHEGO_COMMIT:-$( git rev-list -1 HEAD )}

# Static compilation
static_ld_flags=''
if [ "${STATIC_COMPILATION:-}" = 1 ]
then
    export CC=musl-gcc
    which $CC > /dev/null || ( echo $CC must be available for static compilation && exit 1 )
    static_ld_flags=' -extldflags "-static" -linkmode external '
fi
