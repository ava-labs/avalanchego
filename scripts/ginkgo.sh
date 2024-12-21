#!/usr/bin/env bash

set -euo pipefail

# Run the ginkgo version from go.mod
go run github.com/onsi/ginkgo/v2/ginkgo "${@}"
