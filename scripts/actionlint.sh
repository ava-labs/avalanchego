#!/usr/bin/env bash

set -euo pipefail

go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.1 "${@}"
