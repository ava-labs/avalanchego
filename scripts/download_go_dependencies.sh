#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly REPO_ROOT
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/lib_go_modules.sh"
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/lib_go_tools.sh"

# `go mod download all` pulls a wider set than `go mod tidy` records, so it
# rewrites tools/external/go.sum. This
# script only warms the module cache, so snapshot the checked-in checksums and
# put them back when it finishes. Copies are used rather than `git checkout` so
# a contributor running this locally keeps any unrelated edits. A dirty tree
# breaks any later step that switches branches, which is how the C-Chain
# benchmark jobs publish their results.
checksum_files=("go.work.sum" "tools/external/go.sum")
for prefix in "${TAG_PREFIXES[@]}"; do
  checksum_files+=("${prefix}go.sum")
done

snapshot_dir="$(mktemp -d)"
restore_checksums() {
  local i
  for i in "${!checksum_files[@]}"; do
    if [[ -f "${snapshot_dir}/${i}" ]]; then
      cp "${snapshot_dir}/${i}" "${REPO_ROOT}/${checksum_files[${i}]}"
    fi
  done
  rm -rf "${snapshot_dir}"
}
trap restore_checksums EXIT INT TERM

for i in "${!checksum_files[@]}"; do
  cp "${REPO_ROOT}/${checksum_files[${i}]}" "${snapshot_dir}/${i}"
done

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
