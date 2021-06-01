#!/usr/bin/env bash

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

# Set the PATHS
GOPATH="$(go env GOPATH)"
coreth_path="$GOPATH/pkg/mod/github.com/ava-labs/coreth@$coreth_version"

# Where AvalancheGo binary goes
build_dir="$AVALANCHE_PATH/build"
binary_manager_path="$build_dir/avalanchego"

# Latest Avalanchego binary
latest_avalanchego_path="$build_dir"/avalanchego-latest
latest_avalanchego_process_path="$latest_avalanchego_path/avalanchego-process"
latest_plugin_dir="$latest_avalanchego_path/plugins"
latest_evm_path="$latest_plugin_dir/evm"

# Previous AvalancheGo binary
prev_build_dir="$build_dir/avalanchego-preupgrade" # Where pre-db migration AvalancheGo binary goes
prev_avalanchego_process_path="$prev_build_dir/avalanchego-process"
prev_plugin_dir="$prev_build_dir/plugins"

# Avalabs docker hub
dockerhub_repo="avaplatform/avalanchego"
avalanche_image_name="avalanchego"

# Current branch
current_branch=$(git branch --show-current)

git_commit=${AVALANCHEGO_COMMIT:-$( git rev-list -1 HEAD )}
