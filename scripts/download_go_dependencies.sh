#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly REPO_ROOT
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/lib_go_modules.sh"
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/lib_go_tools.sh"

# Download the workspace build list. This includes dependencies needed by every
# module in go.work, including dependencies used only by tests.
(
  cd "${REPO_ROOT}"
  GOWORK="" go mod download all
)

# Workspace resolution is not the only resolution CI uses. Lint tooling, the
# per-module test suites, and `go mod tidy` all run with GOWORK=off, where each
# module selects its own versions rather than the workspace-wide ones. Download
# those too, so no job has to reach the network for them.
for prefix in "${TAG_PREFIXES[@]}"; do
  (
    cd "${REPO_ROOT}/${prefix}"
    GOWORK=off go mod download all
  )
done

# tools/external is intentionally outside the workspace.
(
  cd "${REPO_ROOT}/tools/external"
  GOWORK=off go mod download all
)

# Tools invoked as `go run pkg@version` resolve their own module graph, so
# downloading the workspace and tool modules does not cover them. Install each
# one to pull that graph into the module cache.
(
  cd "${REPO_ROOT}"
  GOWORK=off go install "${ABIGEN_PKG}"
)
