#!/usr/bin/env bash
set -euo pipefail

# With an explicit root, hash only that module (used by the focused test).
repo_root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
if [[ $# -gt 0 ]]; then
  modules=("${repo_root}")
else
  modules=("${repo_root}" "${repo_root}/graft/coreth" "${repo_root}/graft/evm" "${repo_root}/graft/subnet-evm")
fi

package_json="$(mktemp)"
input_paths="$(mktemp)"
embed_list="$(mktemp)"
trap 'rm -f "${package_json}" "${input_paths}" "${embed_list}"' EXIT

# Git tracks source, module metadata and testdata, including files in grafts.
# Hash contents rather than the commit so unrelated changes do not invalidate
# an exact test cache hit.
git -C "${repo_root}" ls-files -z -- \
  '*.go' '**/*.go' go.mod go.sum go.work.sum '**/go.mod' '**/go.sum' \
  'testdata/**' '**/testdata/**' > "${input_paths}"

# go list reports production and test-only embedded files, regardless of
# extension or whether they live inside a testdata directory.
for module in "${modules[@]}"; do
  (cd "${module}" && go list -json -test ./...) > "${package_json}"
  jq -r '.Dir as $dir | (.EmbedFiles[]?, .TestEmbedFiles[]?, .XTestEmbedFiles[]?) | [$dir, .] | @tsv' \
    < "${package_json}" > "${embed_list}"
  while IFS=$'\t' read -r directory name; do
    file="${directory}/${name}"
    printf '%s\0' "${file#"${repo_root}/"}" >> "${input_paths}"
  done < "${embed_list}"
done

# Include paths as well as file contents so a rename changes the key.
while IFS= read -r -d '' path; do
  printf '%s %s\n' "${path}" "$(git -C "${repo_root}" hash-object "${repo_root}/${path}")"
done < <(sort -zu "${input_paths}") | git hash-object --stdin
