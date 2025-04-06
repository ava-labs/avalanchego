#!/usr/bin/env bash

set -euo pipefail

print_usage() {
  printf "Usage: build [OPTIONS]

  Build avalanchego

  Options:

    -r  Build with race detector
"
}

race=''
while getopts 'r' flag; do
  case "${flag}" in
    r)
      echo "Building with race detection enabled"
      race='-race'
      ;;
    *) print_usage
      exit 1 ;;
  esac
done

AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Configure the build environment
source "$AVALANCHE_PATH"/scripts/constants.sh
# Determine the git commit hash to use for the build
source "$AVALANCHE_PATH"/scripts/git_commit.sh

echo "Downloading dependencies..."
go mod download

echo "Building AvalancheGo with [$(go version)]..."
go build $race -o "$avalanchego_path" \
   -ldflags "-X github.com/ava-labs/avalanchego/version.GitCommit=$git_commit $static_ld_flags" \
   "$AVALANCHE_PATH/main/"*.go
