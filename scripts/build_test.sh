#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set the CGO flags to test the portable version of BLST
CGO_CFLAGS_ALLOW="-O -D__BLST_PORTABLE__"
CGO_CFLAGS="-O -D__BLST_PORTABLE__"

go test -race -timeout="120s" -coverprofile="coverage.out" -covermode="atomic" $(go list ./... | grep -v /mocks | grep -v proto | grep -v tests)
