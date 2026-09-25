#!/usr/bin/env bash
#
# Shared Go module-cache preparation for image builds.
#
# The image Dockerfiles build the repository in workspace mode. Populate the
# host cache with the same workspace module graph before passing it to BuildKit.

function go_module_proxy_cache {
  local proxy_cache
  proxy_cache="$(go env GOMODCACHE)/cache/download"

  if [[ ! -d "${proxy_cache}" ]]; then
    echo "Go module proxy cache not found at ${proxy_cache}" >&2
    return 1
  fi

  printf '%s\n' "${proxy_cache}"
}

function prepare_go_module_cache {
  # CI prepares this cache before it runs an image-build task and then disables
  # external module proxy access. Do not download the modules again.
  if [[ -n "${CI:-}" ]]; then
    return
  fi

  local repo_root=$1
  repo_root="$(cd "${repo_root}" && pwd)"

  if [[ ! -f "${repo_root}/go.work" ]]; then
    echo "go.work not found in ${repo_root}" >&2
    return 1
  fi

  (
    cd "${repo_root}" || return
    GOWORK="${repo_root}/go.work" go mod download
  )
}
