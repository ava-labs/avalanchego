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
  )
done < <(find "${repo_root}" -path "${repo_root}/.git" -prune -o -name go.mod -print0)

manifest="${repo_root}/scripts/go_module_cache_manifest.tsv"
while IFS=$'\t' read -r kind source package; do
  [[ -z "${kind}" || "${kind}" == \#* ]] && continue

  case "${kind}" in
    local)
      (
        cd "${repo_root}/${source}"
        GOWORK=off go list -deps "${package}" >/dev/null
      )
      ;;
    pinned)
      # Pinned tools are outside the repository module graphs. Resolve each
      # graph in an isolated module without building or running the tool.
      temp_module="$(mktemp -d "${repo_root}/.go-module-cache.XXXXXX")"
      (
        trap 'rm -rf "${temp_module}"' EXIT
        cd "${temp_module}"
        go mod init cache-preparation >/dev/null
        go mod edit -require="${source}"
        GOWORK=off go mod download all
        GOWORK=off go list -mod=mod -deps "${package}" >/dev/null
      )
      ;;
    *)
      echo "unknown manifest entry kind: ${kind}" >&2
      exit 1
      ;;
  esac
done < "${manifest}"
