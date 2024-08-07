#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides

CAMINOGO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Where CaminoGo binary goes
build_dir="$CAMINOGO_PATH/build"
CAMINOGO_BIN_PATH="$build_dir/caminogo"
camino_node_symlink_path="$build_dir/camino-node"
plugin_dir="$build_dir/plugins"


# Camino docker hub
# c4tplatform/camino-node - defaults to local as to avoid unintentional pushes
# You should probably set it - export DOCKER_REPO='c4tplatform'
camino_node_dockerhub_repo=${DOCKER_REPO:-"c4tplatform"}"/camino-node"

# Current branch
current_branch_temp=$(git symbolic-ref -q --short HEAD || git describe --tags --always || echo unknown)
# replace / with - to be a docker tag compatible
current_branch=${current_branch_temp////-}

# caminogo and caminoethvm git tag and sha
git_commit=${CAMINO_NODE_COMMIT:-$(git rev-parse --short HEAD)}
git_tag=${CAMINO_NODE_TAG:-$(git describe --tags --abbrev=0 --always || echo unknown)}
caminoethvm_tag=${CAMINO_ETHVM_VERSION:-'v1.1.0-rc4'}
caminoethvm_commit=${CAMINOETHVM_COMMIT:-'07e50749e3c371001c92dff14dc91ef6109a368a'}

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
