#!/usr/bin/env bash
#
# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides
# Use the versions.sh to specify versions
#

CAMINO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# Set the PATHS
GOPATH="$(go env GOPATH)"
coreth_path="$GOPATH/pkg/mod/github.com/chain4travel/coreth@$coreth_version"

# Where CaminoGo binary goes
build_dir="$CAMINO_PATH/build"
caminogo_path="$build_dir/caminogo"
plugin_dir="$build_dir/plugins"
evm_path="$plugin_dir/evm"

# Camino docker hub
# c4tplatform/caminogo - defaults to local as to avoid unintentional pushes
# You should probably set it - export DOCKER_REPO='c4tplatform/caminogo'
caminogo_dockerhub_repo=${DOCKER_REPO:-"caminogo"}

# Current branch
current_branch=$(git symbolic-ref -q --short HEAD || git describe --tags --exact-match || true)

git_commit=${CAMINOGO_COMMIT:-$( git rev-list -1 HEAD )}

# Static compilation
static_ld_flags=''
if [ "${STATIC_COMPILATION:-}" = 1 ]
then
    export CC=musl-gcc
    which $CC > /dev/null || ( echo $CC must be available for static compilation && exit 1 )
    static_ld_flags=' -extldflags "-static" -linkmode external '
fi
