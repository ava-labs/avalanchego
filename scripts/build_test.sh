#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

go test -race -timeout="4m" -coverprofile="coverage.out" -covermode="atomic" ./...
