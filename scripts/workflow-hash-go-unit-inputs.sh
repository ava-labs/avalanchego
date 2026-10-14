#!/usr/bin/env bash
set -euo pipefail

# Hash unit-test inputs so a master run can save results when those inputs
# change, including embedded files outside testdata directories.
# Examples:
#   ./scripts/workflow-hash-go-unit-inputs.sh
#   ./scripts/workflow-hash-go-unit-inputs.sh /path/to/test-module
# With an explicit root, hash only that module (used by the focused test).
REPO_ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
if [[ $# -gt 0 ]]; then
  MODULES=("${REPO_ROOT}")
else
  MODULES=("${REPO_ROOT}" "${REPO_ROOT}/graft/coreth" "${REPO_ROOT}/graft/evm" "${REPO_ROOT}/graft/subnet-evm")
fi

PACKAGE_JSON="$(mktemp)"
INPUT_PATHS="$(mktemp)"
EMBED_LIST="$(mktemp)"
trap 'rm -f "${PACKAGE_JSON}" "${INPUT_PATHS}" "${EMBED_LIST}"' EXIT

# Git tracks source, module metadata and testdata, including files in grafts.
# Hash contents rather than the commit so unrelated changes do not invalidate
# an exact test cache hit.
git -C "${REPO_ROOT}" ls-files -z -- \
  '*.go' '**/*.go' go.mod go.sum go.work.sum '**/go.mod' '**/go.sum' \
  'testdata/**' '**/testdata/**' > "${INPUT_PATHS}"

# go list reports production and test-only embedded files, regardless of
# extension or whether they live inside a testdata directory.
for module in "${MODULES[@]}"; do
  (cd "${module}" && go list -json -test ./...) > "${PACKAGE_JSON}"
  jq -r '.Dir as $dir | (.EmbedFiles[]?, .TestEmbedFiles[]?, .XTestEmbedFiles[]?) | [$dir, .] | @tsv' \
    < "${PACKAGE_JSON}" > "${EMBED_LIST}"
  while IFS=$'\t' read -r directory name; do
    file="${directory}/${name}"
    printf '%s\0' "${file#"${REPO_ROOT}/"}" >> "${INPUT_PATHS}"
  done < "${EMBED_LIST}"
done

# Include paths as well as file contents so a rename changes the key.
while IFS= read -r -d '' input_path; do
  printf '%s %s\n' "${input_path}" "$(git -C "${REPO_ROOT}" hash-object "${REPO_ROOT}/${input_path}")"
done < <(sort -zu "${INPUT_PATHS}") | git hash-object --stdin
