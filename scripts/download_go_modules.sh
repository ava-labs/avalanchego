#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root

# Download each module outside workspace mode. Some CI tasks run from a graft
# module or disable workspace mode, which can select dependency versions that
# differ from the workspace build list.
while IFS= read -r -d '' go_mod; do
  module_dir="$(dirname "${go_mod}")"
  (
    cd "${module_dir}"
    # tidy -diff resolves the modules required by package tests as well as
    # ordinary builds, without modifying go.mod or go.sum.
    GOWORK=off go mod tidy -diff

    # A module's declared tools can use dependency versions outside its package
    # build list. Build each tool once to download that tool's full graph.
    while IFS= read -r tool_path; do
      GOWORK=off go tool -modfile=go.mod "${tool_path##*/}" -h \
        >/dev/null 2>&1
    done < <(go mod edit -json | jq -r '.Tool[]?.Path')
  )
done < <(find "${repo_root}" -path "${repo_root}/.git" -prune -o -name go.mod -print0)

# The load-contract generator runs this pinned tool from GOMODCACHE. It is not
# a requirement of a repository module, so download it explicitly.
abigen_version="$(
  sed -n -E "s/^readonly abigen_version='([^']+)'$/\\1/p" \
    "${repo_root}/tests/load/contracts/generate_abi_bindings.sh"
)"
if [[ -z "${abigen_version}" ]]; then
  echo "abigen version not found" >&2
  exit 1
fi
go mod download "github.com/ava-labs/libevm@${abigen_version}"
abigen_dir="$(go env GOMODCACHE)/github.com/ava-labs/libevm@${abigen_version}"
(
  cd "${abigen_dir}"
  go run ./cmd/abigen --help >/dev/null
)
