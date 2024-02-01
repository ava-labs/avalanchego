#!/usr/bin/env bash
#
# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides
# Use the versions.sh to specify versions
#

# Set the PATHS
GOPATH="$(go env GOPATH)"

# Where CaminoGo binary goes
build_dir="$CAMINO_NODE_PATH/build"
camino_node_path="$build_dir/camino-node"
plugin_dir="$build_dir/plugins"

# Camino docker hub
# c4tplatform/camino-node - defaults to local as to avoid unintentional pushes
# You should probably set it - export DOCKER_REPO='c4tplatform'
camino_node_dockerhub_repo=${DOCKER_REPO:-"c4tplatform"}"/camino-node"

# Current branch
current_branch=$(git symbolic-ref -q --short HEAD || git describe --tags || echo unknown)

git_commit=${CAMINO_NODE_COMMIT:-$(git rev-parse --short HEAD)}
git_tag=${CAMINO_NODE_TAG:-$(git describe --tags --abbrev=0 || echo unknown)}

# caminogo and caminoethvm git tag and sha
oldDir=$(pwd) && cd $CAMINO_NODE_PATH/dependencies/caminoethvm
caminoethvm_commit=${CAMINOETHVM_COMMIT:-$( git rev-parse --short HEAD )}
caminoethvm_tag=$(git describe --tags --abbrev=0 || echo unknown)
cd $CAMINO_NODE_PATH/dependencies/caminoethvm/caminogo
caminogo_commit=${CAMINOGO_COMMIT:-$( git rev-parse --short HEAD )}
caminogo_tag=$(git describe --tags --abbrev=0 || echo unknown)
cd $oldDir

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
