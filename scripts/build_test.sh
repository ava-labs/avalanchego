#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

go test -race -timeout="180s" -coverprofile="coverage.out" -covermode="atomic" ./plugin/... ./core/... ./eth/... ./tests/...
