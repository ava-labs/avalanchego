#!/usr/bin/env bash

set -euo pipefail

# If an explicit version is not specified, go run uses the ginkgo version from go.mod
go run github.com/onsi/ginkgo/v2/ginkgo "${@}"
