#!/usr/bin/env bash

set -euo pipefail


# Ensure required tools are installed
if ! command -v solc &> /dev/null; then
  echo "Error: solc not found. Run this command from Nix shell."
  exit 1
fi

CONTRACTS_DIR="$(dirname "$0")"

for FILE in "${CONTRACTS_DIR}"/*.sol; do
  echo "Generating Go bindings from Solidity contract $FILE..."
  CONTRACT_NAME=$(basename "$FILE" .sol)
  solc --abi --bin --overwrite -o "$CONTRACTS_DIR" "${CONTRACTS_DIR}/${CONTRACT_NAME}.sol"
  go run github.com/ava-labs/libevm/cmd/abigen@latest \
    --bin="${CONTRACTS_DIR}/${CONTRACT_NAME}.bin" \
    --abi="${CONTRACTS_DIR}/${CONTRACT_NAME}.abi" \
    --type "${CONTRACT_NAME}" \
    --pkg=contracts \
    --out="${CONTRACTS_DIR}/${CONTRACT_NAME}.bindings.go"
  rm "${CONTRACTS_DIR}/${CONTRACT_NAME}.bin" "${CONTRACTS_DIR}/${CONTRACT_NAME}.abi"
  echo "Generated ${CONTRACT_NAME}.bindings.go"
done

