#!/usr/bin/env bash

# Set the PATHS
GOPATH="$(go env GOPATH)"

# Avalabs docker hub
dockerhub_repo="avaplatform/avalanchego"

# Current branch
current_branch=${CURRENT_BRANCH:-$(git describe --tags --exact-match 2> /dev/null || git symbolic-ref -q --short HEAD || git rev-parse --short HEAD)}
echo "Using branch: ${current_branch}"

# Image build id
# Use an abbreviated version of the full commit to tag the image.

# WARNING: this will use the most recent commit even if there are un-committed changes present
subnet_evm_commit="$(git --git-dir="$SUBNET_EVM_PATH/.git" rev-parse HEAD)"
subnet_evm_commit_id="${subnet_evm_commit::8}"

build_image_id=${BUILD_IMAGE_ID:-"$avalanche_version-$subnet_evm_commit_id"}
