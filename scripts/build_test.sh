#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

go test -tags rocksdballowed -race -timeout="120s" -coverprofile="coverage.out" -covermode="atomic" $(go list ./... | grep -v /mocks | grep -v proto)
