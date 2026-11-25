#!/usr/bin/env bash
set -euo pipefail

# Shared source/embed discovery. Cache-specific entrypoints supply their own
# configuration paths; this helper does not select unit or E2E policy.
if [[ $# -lt 1 ]]; then
  echo "usage: $0 <repository-root> [configuration-pathspec...]" >&2
  exit 2
fi
REPO_ROOT="$1"
shift
MODULES=("${REPO_ROOT}")
for module in graft/coreth graft/evm graft/subnet-evm; do
  if [[ -f "${REPO_ROOT}/${module}/go.mod" ]]; then
    MODULES+=("${REPO_ROOT}/${module}")
  fi
done

PACKAGE_JSON="$(mktemp)"
INPUT_PATHS="$(mktemp)"
EMBED_LIST="$(mktemp)"
trap 'rm -f "${PACKAGE_JSON}" "${INPUT_PATHS}" "${EMBED_LIST}"' EXIT

# Include test configuration as well as source, metadata, and fixtures.
# Go safely reruns tests when flags change, but our exact-hit validator rejects
# uncached results. Configuration changes must therefore select a new archive.
# Hash contents rather than the commit so unrelated changes keep exact hits.
git -C "${REPO_ROOT}" ls-files -z -- \
  '*.go' '**/*.go' go.mod go.sum go.work go.work.sum '**/go.mod' '**/go.sum' \
  'testdata/**' '**/testdata/**' \
  flake.nix flake.lock 'nix/go/**' \
  .github/actions/setup-go-for-project/action.yml \
  scripts/constants.sh scripts/run_task.sh scripts/nix_run.sh \
  scripts/workflow-hash-go-cache-inputs.sh "$@" > "${INPUT_PATHS}"

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
# Git's --stdin-paths accepts newline-delimited paths, so this assumes repo
# paths do not contain newlines.
sorted_paths() { sort -zu "${INPUT_PATHS}" | tr '\0' '\n'; }
{
  paste -d' ' \
    <(sorted_paths) \
    <(sorted_paths | git -C "${REPO_ROOT}" hash-object --stdin-paths)
  # Root settings apply to both task groups. Hash them, not unrelated tasks.
  # Cache-specific task definitions live in separate included Taskfiles.
  printf 'Taskfile globals %s\n' "$(awk '/^tasks:/{exit} {print}' "${REPO_ROOT}/Taskfile.yml" | git hash-object --stdin)"
} | git hash-object --stdin
