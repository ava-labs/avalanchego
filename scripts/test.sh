#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"
# While CGO_ENABLED doesn't need to be explicitly set, it produces a much more
# clear error due to the default value change in go1.20.
export CGO_ENABLED=1

go test "${1-}" -timeout="120s" -coverprofile="coverage.out" -covermode="atomic" "$(go list ./... | grep -v /mocks | grep -v proto | grep -v tests)"
