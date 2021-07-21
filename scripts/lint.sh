#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

golangci-lint run --max-same-issues=0 --timeout=2m
