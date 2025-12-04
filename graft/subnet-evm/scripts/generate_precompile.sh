#!/usr/bin/env bash
set -e

# This script generates a Stateful Precompile stub based off of a Solidity ABI file.
# It first sets the necessary CGO_FLAGs for the BLST library used in AvalancheGo and
# then runs PrecompileGen.
if ! [[ "$0" =~ scripts/generate_precompile.sh ]]; then
  echo "must be run from repository root, but got $0"
  exit 255
fi

SUBNET_EVM_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Load the constants
source "$SUBNET_EVM_PATH"/scripts/constants.sh

go run ./cmd/precompilegen/main.go "$@"
