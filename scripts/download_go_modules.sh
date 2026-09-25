#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
readonly REPO_ROOT

# Download each module outside workspace mode. Some CI tasks run from a graft
# module or disable workspace mode, which can select dependency versions that
# differ from the workspace build list. Do not treat a repository-local module
# cache as a repository module when validating an offline cache.
GO_MOD_CACHE="$(go env GOMODCACHE)"
if [[ -d "${GO_MOD_CACHE}" ]]; then
  GO_MOD_CACHE="$(cd "${GO_MOD_CACHE}" && pwd -P)"
fi
while IFS= read -r -d '' GO_MOD; do
  MODULE_DIR="$(dirname "${GO_MOD}")"
  (
    cd "${MODULE_DIR}"
    # tidy -diff resolves the modules required by package tests as well as
    # ordinary builds, without modifying go.mod or go.sum.
    GOWORK=off go mod tidy -diff
  )
done < <(find "${REPO_ROOT}" \
  \( -path "${REPO_ROOT}/.git" -o -path "${GO_MOD_CACHE}" \) -prune -o \
  -name go.mod -print0)

MANIFEST="${REPO_ROOT}/scripts/go_module_cache_manifest.tsv"
while IFS=$'\t' read -r KIND SOURCE PACKAGE; do
  [[ -z "${KIND}" || "${KIND}" == \#* ]] && continue

  case "${KIND}" in
    local)
      (
        cd "${REPO_ROOT}/${SOURCE}"
        GOWORK=off go list -deps "${PACKAGE}" >/dev/null
      )
      ;;
    pinned)
      # Pinned tools are outside the repository module graphs. Resolve each
      # graph in an isolated module without building or running the tool.
      TEMP_MODULE="$(mktemp -d "${REPO_ROOT}/.go-module-cache.XXXXXX")"
      (
        trap 'rm -rf "${TEMP_MODULE}"' EXIT
        cd "${TEMP_MODULE}"
        go mod init cache-preparation >/dev/null
        go mod edit -require="${SOURCE}"
        # Download the declared module before resolving its graph. Minimal
        # version selection can otherwise replace a pinned module with a newer
        # version and leave the declared archive out of the cache.
        GOWORK=off go mod download "${SOURCE}"
        GOWORK=off go mod download all
        GOWORK=off go list -mod=mod -deps "${PACKAGE}" >/dev/null
      )
      ;;
    *)
      echo "unknown manifest entry kind: ${KIND}" >&2
      exit 1
      ;;
  esac
done < "${MANIFEST}"

# sync-go-work runs this in workspace mode. It can select modules that are not
# part of any individual module's package graph.
GOWORK='' go mod download
