#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)
cd "${REPO_ROOT}"

go tool ginkgo "${@}"
