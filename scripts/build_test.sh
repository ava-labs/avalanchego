#!/bin/bash -e

# Ted: contact me when you make any changes

SRC_DIR="$(dirname "${BASH_SOURCE[0]}")"
source "$SRC_DIR/env.sh"

go test -race -timeout="30s" -coverprofile="coverage.out" -covermode="atomic" ./...
