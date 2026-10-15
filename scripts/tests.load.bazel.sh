#!/usr/bin/env bash

set -euo pipefail

# Run the Bazel-built load-test binary with a Bazel-built AvalancheGo binary.
if (( $# < 2 )); then
  echo "usage: $0 <avalanchego> <load-test> [load-test-args...]" >&2
  exit 1
fi

AVALANCHEGO_PATH="$(realpath "$1")"
LOAD_TEST_PATH="$(realpath "$2")"
shift 2

exec "${LOAD_TEST_PATH}" --avalanchego-path="${AVALANCHEGO_PATH}" "$@"
