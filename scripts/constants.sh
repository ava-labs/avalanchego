#!/usr/bin/env bash

# Set the PATHS
GOPATH="$(go env GOPATH)"

# Set default binary location
binary_path="$GOPATH/src/github.com/ava-labs/avalanchego/build/plugins/evm"

# Avalabs docker hub
dockerhub_repo="avaplatform/avalanchego"

# Current branch
current_branch=${CURRENT_BRANCH:-$(git branch --show-current)}
echo "Using branch: ${current_branch}"
