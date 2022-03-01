#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

export GOGC=25

go test -p 1 -v -race -timeout="20m" -coverprofile="coverage.out" -covermode="atomic" ./...
