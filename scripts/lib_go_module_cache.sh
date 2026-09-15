#!/usr/bin/env bash
#
# Shared Go module-cache preparation for image builds.
#
# The image Dockerfiles build the repository in workspace mode. Populate the
# host cache with the same workspace module graph before passing it to BuildKit.

function prepare_go_module_cache {
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
