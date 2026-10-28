#!/usr/bin/env bash

set -euo pipefail

# Run the release-upgrade test from Bazel-built AvalancheGo, Ginkgo, and test binaries.
if (( $# < 3 )); then
  echo "usage: $0 <avalanchego> <upgrade-test> <ginkgo> [upgrade-test-args...]" >&2
  exit 1
fi

AVALANCHEGO_PATH="$(realpath "$1")"
E2E_TARGET="$(realpath "$2")"
GINKGO_PATH="$(realpath "$3")"
export AVALANCHEGO_PATH E2E_TARGET GINKGO_PATH
shift 3

cd "${BUILD_WORKSPACE_DIRECTORY:-${PWD}}"
exec ./scripts/tests.upgrade.sh "$@"
