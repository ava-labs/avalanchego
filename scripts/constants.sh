#!/usr/bin/env bash

# Set the PATHS
GOPATH="$(go env GOPATH)"
coreth_path="$GOPATH/pkg/mod/github.com/ava-labs/coreth@$coreth_version"

# Where AvalancheGo binary goes
build_dir="$AVALANCHE_PATH/build"

# Where plugin binaries (namely coreth) go
plugin_dir="$build_dir/plugins"
evm_path="$plugin_dir/evm"

# Avalabs docker hub
dockerhub_repo="avaplatform/avalanchego"
avalanche_image_name="avalanchego"

# Current branch
current_branch=$(git branch --show-current)
