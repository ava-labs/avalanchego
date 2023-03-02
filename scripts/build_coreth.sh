#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

race=''
coreth_path=''
evm_path=''

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

print_usage() {
  printf "Usage: build_coreth [OPTIONS]

  Build coreth

  Options:
    -r  Build with race detector (optional)
    -c  Coreth path (optional; must be provided with -c)
    -e  EVM path (optional; must be provided with -e)
"
}

while getopts 'rc:e:' flag; do
  case "${flag}" in
    r) race='-race' ;;
    c) coreth_path=${OPTARG} ;;
    e) evm_path=${OPTARG} ;;
    *) print_usage
      exit 1 ;;
  esac
done

# Sanity-check the user's overrides for coreth path/version if they supplied a flag
if [[ -z $coreth_path ]] || [[ -z $evm_path ]]; then
  echo "Invalid arguments to build coreth. Coreth path (-c) must be provided with EVM path (-e)."
  print_usage
  exit 1
fi

if [[ ! -d "$coreth_path" ]]; then
  go get "github.com/ava-labs/coreth@$coreth_version"
fi

# Build Coreth
build_args="$race"
echo "Building Coreth @ ${coreth_version} ..."
cd "$coreth_path"
go build $build_args -ldflags "-X github.com/ava-labs/coreth/plugin/evm.Version=$coreth_version $static_ld_flags" -o "$evm_path" "plugin/"*.go
cd "$AVALANCHE_PATH"

# Building coreth + using go get can mess with the go.mod file.
go mod tidy -compat=1.19

# Exit build successfully if the Coreth EVM binary is created successfully
if [[ -f "$evm_path" ]]; then
        echo "Coreth Build Successful"
        exit 0
else
        echo "Coreth Build Failure" >&2
        exit 1
fi
