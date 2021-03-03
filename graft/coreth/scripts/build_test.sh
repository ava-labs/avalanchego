#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

go test -race -timeout="120s" -coverprofile="coverage.out" -covermode="atomic" ./plugin/...
