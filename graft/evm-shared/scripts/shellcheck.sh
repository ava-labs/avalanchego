#!/usr/bin/env bash

set -euo pipefail

VERSION="v0.9.0"

function get_version {
  local target_path=$1
  if command -v "${target_path}" > /dev/null; then
    echo "v$("${target_path}" --version | grep version: | awk '{print $2}')"
  fi
}

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

SYSTEM_VERSION="$(get_version shellcheck)"
if [[ "${SYSTEM_VERSION}" == "${VERSION}" ]]; then
  SHELLCHECK=shellcheck
else
  # Try to install a local version
  SHELLCHECK="${REPO_ROOT}/bin/shellcheck"
  LOCAL_VERSION="$(get_version "${SHELLCHECK}")"
  if [[ -z "${LOCAL_VERSION}" || "${LOCAL_VERSION}" != "${VERSION}" ]]; then
    if which sw_vers &> /dev/null; then
      echo "on macos, only x86_64 binaries are available so rosetta is required"
      echo "to avoid using rosetta, install via homebrew: brew install shellcheck"
      DIST=darwin.x86_64
    else
      # Linux - binaries for common arches *should* be available
      arch="$(uname -i)"
      DIST="linux.${arch}"
    fi
    curl -s -L "https://github.com/koalaman/shellcheck/releases/download/${VERSION}/shellcheck-${VERSION}.${DIST}.tar.xz" | tar Jxv -C /tmp > /dev/null
    mkdir -p "$(dirname "${SHELLCHECK}")"
    cp /tmp/shellcheck-"${VERSION}"/shellcheck "${SHELLCHECK}"
  fi
fi

find "${REPO_ROOT}" -type f -name "*.sh" -print0 | xargs -0 "${SHELLCHECK}" "${@}"
