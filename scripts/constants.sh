#!/usr/bin/env bash

GOPATH="$(go env GOPATH)"
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
CORETH_VER="v0.5.3-rc.2" # Current version of Coreth to use
PREUPGRADE_AVALANCHEGO_VER="v1.4.5-preupgrade.4" # Release of AvalancheGo compatible with previous database version
CORETH_PATH="$GOPATH/pkg/mod/github.com/ava-labs/coreth@$CORETH_VER"

BUILD_DIR="$AVALANCHE_PATH/build/avalanchego-latest" # Where AvalancheGo binary goes
PLUGIN_DIR="$BUILD_DIR/plugins" # Where plugin binaries (namely coreth) go
EVM_PATH="$PLUGIN_DIR/evm"
AVALANCHEGO_PROCESS_PATH="$BUILD_DIR/avalanchego-process"
BINARY_MANAGER_PATH="$AVALANCHE_PATH/build/avalanchego"

PREV_BUILD_DIR="$AVALANCHE_PATH/build/avalanchego-preupgrade" # Where pre-db migration AvalancheGo binary goes
PREV_PLUGIN_DIR="$PREV_BUILD_DIR/plugins"
PREV_AVALANCHEGO_PROCESS_PATH="$PREV_BUILD_DIR/avalanchego-process"
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
