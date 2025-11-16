#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

if [ ! -f "coverage.out" ]; then
  echo "no coverage file"
  exit 0
fi

totalCoverage=$(go tool cover -func=coverage.out | grep total | grep -Eo '[0-9]+\.[0-9]+')
echo "Current test coverage : $totalCoverage %"
echo "========================================"

go tool cover -func=coverage.out
