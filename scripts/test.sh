#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CGO_CFLAGS="-O -D__BLST_PORTABLE__" go test ${1-} -timeout="120s" -coverprofile="coverage.out" -covermode="atomic" $(go list ./... | grep -v /mocks | grep -v proto | grep -v tests)
