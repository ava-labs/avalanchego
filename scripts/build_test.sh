#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Ted: contact me when you make any changes

go test -race -timeout="60s" -coverprofile="coverage.out" -covermode="atomic" ./...
