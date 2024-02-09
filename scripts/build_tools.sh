#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

echo "Building tools..."

# Camino-Node root folder
CAMINOGO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$CAMINOGO_PATH"/scripts/constants.sh

echo "Downloading dependencies..."
(cd $CAMINOGO_PATH && go mod download)

# Create tools directory
tools_dir=$build_dir/tools/
mkdir -p $tools_dir

echo "Building cert tool..."
go build -ldflags="-s -w" -o "$tools_dir/cert" "$CAMINOGO_PATH/tools/cert/"*.go

echo "Building camino-network-runner tool..."
$CAMINOGO_PATH/tools/camino-network-runner/scripts/build.sh "$tools_dir"