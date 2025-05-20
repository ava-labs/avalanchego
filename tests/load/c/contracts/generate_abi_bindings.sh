#!/usr/bin/env bash

set -euo pipefail


# Ensure required tools are installed
if ! command -v solc &> /dev/null; then
  echo "Error: solc not found. Run this command from Nix shell."
  exit 1
fi

CONTRACTS_DIR="$(dirname "$0")"
TEMPDIR=$(mktemp -d)
for FILE in "${CONTRACTS_DIR}"/*.sol; do
  echo "Generating Go bindings from Solidity contract $FILE..."
  CONTRACT_NAME=$(basename "$FILE" .sol)
  solc --abi --bin --overwrite -o "$TEMPDIR" "${CONTRACTS_DIR}/${CONTRACT_NAME}.sol"
  go run github.com/ava-labs/libevm/cmd/abigen@latest \
    --bin="${TEMPDIR}/${CONTRACT_NAME}.bin" \
    --abi="${TEMPDIR}/${CONTRACT_NAME}.abi" \
    --type $CONTRACT_NAME \
    --pkg=contracts \
    --out="${CONTRACTS_DIR}/${CONTRACT_NAME}.bindings.go"
  echo "Generated ${CONTRACT_NAME}.bindings.go"
done
rm -r "${TEMPDIR}"
