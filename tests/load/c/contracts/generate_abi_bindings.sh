#!/usr/bin/env bash

set -euo pipefail


# Ensure required tools are installed
if ! command -v solc &> /dev/null; then
  echo "Error: solc not found. Run this command from Nix shell."
  exit 1
fi

CONTRACTS_DIR="$(dirname "$0")"
TEMPDIR=$(mktemp -d)

cleanup() {
  rm -r "${TEMPDIR}"   
}

trap cleanup EXIT

# List of .sol files to ignore creating Go bindings for
HELPER_FILES=("Dummy.sol")

should_skip() {
  local file="$1"
  for helper_file in "${HELPER_FILES[@]}"; do
    if [[ "$file" == "$helper_file" ]]; then
      return 0 
    fi
  done
  return 1
}

for FILE in "${CONTRACTS_DIR}"/*.sol; do
  FILE_NAME=$(basename "$FILE")
  if should_skip "$FILE_NAME"; then
    echo "Skipping $FILE_NAME"
    continue
  fi


  echo "Generating Go bindings from Solidity contract $FILE..."
  CONTRACT_NAME=$(basename "$FILE" .sol)
  solc --abi --bin --overwrite -o "$TEMPDIR" "${CONTRACTS_DIR}/${CONTRACT_NAME}.sol"
  go run github.com/ava-labs/libevm/cmd/abigen@v1.13.14-0.2.0.release \
    --bin="${TEMPDIR}/${CONTRACT_NAME}.bin" \
    --abi="${TEMPDIR}/${CONTRACT_NAME}.abi" \
    --type "$CONTRACT_NAME" \
    --pkg=contracts \
    --out="${CONTRACTS_DIR}/${CONTRACT_NAME}.bindings.go"
  echo "Generated ${CONTRACT_NAME}.bindings.go"
done
