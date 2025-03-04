#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

# Set the PATHS
GOPATH="$(go env GOPATH)"
DEFAULT_PLUGIN_DIR="${HOME}/.avalanchego/plugins"
DEFAULT_VM_NAME="subnet-evm"
DEFAULT_VM_ID="srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"

# Avalabs docker hub
# avaplatform/avalanchego - defaults to local as to avoid unintentional pushes
# You should probably set it - export IMAGE_NAME='avaplatform/subnet-evm'
IMAGE_NAME=${IMAGE_NAME:-"subnet-evm"}

# Shared between ./scripts/build_docker_image.sh and ./scripts/tests.build_docker_image.sh
AVALANCHEGO_IMAGE_NAME="${AVALANCHEGO_IMAGE_NAME:-avaplatform/avalanchego}"

# if this isn't a git repository (say building from a release), don't set our git constants.
if [ ! -d .git ]; then
    CURRENT_BRANCH=""
    SUBNET_EVM_COMMIT=""
else
    # Current branch
    CURRENT_BRANCH=${CURRENT_BRANCH:-$(git describe --tags --exact-match 2>/dev/null || git symbolic-ref -q --short HEAD || git rev-parse --short HEAD || :)}

    # Image build id
    #
    # Use an abbreviated version of the full commit to tag the image.
    # WARNING: this will use the most recent commit even if there are un-committed changes present
    SUBNET_EVM_COMMIT="$(git --git-dir="$SUBNET_EVM_PATH/.git" rev-parse HEAD || :)"
fi

# Shared between ./scripts/build_docker_image.sh and ./scripts/tests.build_docker_image.sh
DOCKERHUB_TAG=${SUBNET_EVM_COMMIT::8}

echo "Using branch: ${CURRENT_BRANCH}"

# Static compilation
STATIC_LD_FLAGS=''
if [ "${STATIC_COMPILATION:-}" = 1 ]; then
    export CC=musl-gcc
    command -v $CC || (echo $CC must be available for static compilation && exit 1)
    STATIC_LD_FLAGS=' -extldflags "-static" -linkmode external '
fi

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O2 -D__BLST_PORTABLE__"

# CGO_ENABLED is required for multi-arch builds.
export CGO_ENABLED=1
