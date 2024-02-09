#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

echo "Building Genesis Generator..."

CAMINOGO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
source "$CAMINOGO_PATH"/scripts/constants.sh

# Load the constants
source "$CAMINOGO_PATH"/scripts/constants.sh

echo "Downloading dependencies..."
(cd $CAMINOGO_PATH && go mod download)

# Create tools directory
tools_dir=$build_dir/tools/
mkdir -p $tools_dir

target="$tools_dir/genesis-generator"
go build -ldflags="-s -w" -o "$target" "$CAMINOGO_PATH/tools/genesis/"*.go
