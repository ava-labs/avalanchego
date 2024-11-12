#!/usr/bin/env bash

set -euo pipefail

go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.1

actionlint
