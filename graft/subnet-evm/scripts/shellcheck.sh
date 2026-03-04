#!/usr/bin/env bash

set -euo pipefail

VERSION="v0.9.0"

# Scripts that are sourced from upstream and not maintained in this repo will not be shellchecked.
# Also ignore the local avalanchego clone, git submodules, and node_modules.
IGNORED_FILES="
  metrics/validate.sh
  avalanchego/*
  contracts/lib/*
  contracts/node_modules/*
"

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

IGNORED_CONDITIONS=()
for file in ${IGNORED_FILES}; do
  if [[ -n "${IGNORED_CONDITIONS-}" ]]; then
    IGNORED_CONDITIONS+=(-o)
  fi
  IGNORED_CONDITIONS+=(-path "${REPO_ROOT}/${file}" -prune)
done

find "${REPO_ROOT}" \( "${IGNORED_CONDITIONS[@]}" \) -o -type f -name "*.sh" -print0 | xargs -0 "${SHELLCHECK}" "${@}"
