#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

export GOGC=25

# We pass in the arguments to this script directly to enable easily passing parameters such as enabling race detection,
# parallelism, and test coverage.
# DO NOT RUN "tests/e2e" since it's run by ginkgo
go test -coverprofile=coverage.out -covermode=atomic -timeout="30m" $@ $(go list ./... | grep -v tests/e2e)
