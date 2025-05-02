#!/usr/bin/env bash

set -euo pipefail

# Define paths
CONTRACTS_DIR="$(dirname "$0")/.." # contracts/ folder
BUILD_DIR="$CONTRACTS_DIR/build"
ABI_BINDINGS_DIR="$CONTRACTS_DIR/abi-bindings"
CONTRACT_NAME="EVMLoadSimulator"
SOL_FILE="$CONTRACTS_DIR/$CONTRACT_NAME.sol"

# Ensure required tools are installed
if ! command -v solc &> /dev/null; then
  echo "Error: solc (Solidity compiler) is not installed."
  exit 1
fi

if ! command -v abigen &> /dev/null; then
  echo "Error: abigen (from coreth) is not installed."
  exit 1
fi

# Create necessary directories
mkdir -p "$BUILD_DIR"
mkdir -p "$ABI_BINDINGS_DIR"

# Compile the Solidity contract
echo "Compiling $CONTRACT_NAME.sol..."
solc --abi --bin --overwrite -o "$BUILD_DIR" "$SOL_FILE"

# Check if compilation was successful
if [[ ! -f "$BUILD_DIR/$CONTRACT_NAME.abi" || ! -f "$BUILD_DIR/$CONTRACT_NAME.bin" ]]; then
  echo "Error: Compilation failed. ABI or binary file not found."
  exit 1
fi

# Generate Go ABI bindings
echo "Generating Go ABI bindings..."
abigen \
  --bin="$BUILD_DIR/$CONTRACT_NAME.bin" \
  --abi="$BUILD_DIR/$CONTRACT_NAME.abi" \
  --type $CONTRACT_NAME \
  --pkg=evmloadsimulator \
  --out="$ABI_BINDINGS_DIR/$CONTRACT_NAME.go"

# Check if ABI bindings were generated successfully
if [[ ! -f "$ABI_BINDINGS_DIR/$CONTRACT_NAME.go" ]]; then
  echo "Error: Failed to generate Go ABI bindings."
  exit 1
fi

echo "Successfully generated Go ABI bindings at $ABI_BINDINGS_DIR/$CONTRACT_NAME.go"
