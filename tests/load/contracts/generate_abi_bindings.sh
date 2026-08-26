#!/usr/bin/env bash

set -euo pipefail


# Ensure required tools are installed
if ! command -v solc &> /dev/null; then
  echo "Error: solc not found. Run this command from Nix shell."
  exit 1
fi

CONTRACTS_DIR="$(dirname "$0")"
REPO_ROOT="$(cd "${CONTRACTS_DIR}/../../.." && pwd)"
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/lib_go_tools.sh"
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

# `go run pkg@version` always queries the module proxy for the module's
# deprecation notice, which is a @latest query rather than a pinned lookup. No
# amount of module-cache warming satisfies it, so it cannot run under the
# GOPROXY=off that CI test jobs use. CI therefore builds the pinned binary in
# its setup job and points ABIGEN_BIN at it. Locally, `go run` is fine.
if [[ -n "${ABIGEN_BIN:-}" && -x "${ABIGEN_BIN}" ]]; then
  ABIGEN=("${ABIGEN_BIN}")
else
  ABIGEN=(go run "${ABIGEN_PKG}")
fi

for FILE in "${CONTRACTS_DIR}"/*.sol; do
  FILE_NAME=$(basename "$FILE")
  if should_skip "$FILE_NAME"; then
    echo "Skipping $FILE_NAME"
    continue
  fi


  echo "Generating Go bindings from Solidity contract $FILE..."
  CONTRACT_NAME=$(basename "$FILE" .sol)
  solc --evm-version="cancun" --abi --bin --overwrite -o "$TEMPDIR" "${CONTRACTS_DIR}/${CONTRACT_NAME}.sol"
  "${ABIGEN[@]}" \
    --bin="${TEMPDIR}/${CONTRACT_NAME}.bin" \
    --abi="${TEMPDIR}/${CONTRACT_NAME}.abi" \
    --type "$CONTRACT_NAME" \
    --pkg=contracts \
    --out="${CONTRACTS_DIR}/${CONTRACT_NAME}.bindings.go"
  echo "Generated ${CONTRACT_NAME}.bindings.go"
done
