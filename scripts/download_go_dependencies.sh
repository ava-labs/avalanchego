#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root
# Download the workspace build list. This includes dependencies needed by every
# module in go.work, including dependencies used only by tests.
(
  cd "${repo_root}"
  GOWORK="" go mod download all
)

# tools/external is intentionally outside the workspace.
(
  cd "${repo_root}/tools/external"
  GOWORK=off go mod download all
)
