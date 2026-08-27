#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_e2e_test_artifacts.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/constants.sh

race=''
if [[ "${1:-}" == '-r' ]]; then
  race='-race'
fi

mkdir -p build

echo "Building Ginkgo CLI..."
go build -o ./build/ginkgo github.com/onsi/ginkgo/v2/ginkgo

echo "Building E2E test binary..."
go test ${race} -c -o ./build/e2e.test ./tests/e2e

echo "Building upgrade test binary..."
go test ${race} -c -o ./build/upgrade.test ./tests/upgrade
