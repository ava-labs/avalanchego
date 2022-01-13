#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

golangci-lint run --path-prefix=. --timeout 3m
