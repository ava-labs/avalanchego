#!/usr/bin/env bash

set -euo pipefail

# This script can also be used to correct the problems detected by shellcheck by invoking as follows:
#
# ./scripts/tests.shellcheck.sh -f diff | git apply
#

if ! [[ "$0" =~ scripts/shellcheck.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

VERSION="v0.9.0"

function get_version {
  local target_path=$1
  if command -v "${target_path}" > /dev/null; then
    echo "v$("${target_path}" --version | grep version: | awk '{print $2}')"
  fi
}

SYSTEM_VERSION="$(get_version shellcheck)"
if [[ "${SYSTEM_VERSION}" == "${VERSION}" ]]; then
  SHELLCHECK=shellcheck
else
  # Try to install a local version
  SHELLCHECK=./bin/shellcheck
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

# `find *` is the simplest way to ensure find does not include a
# leading `.` in filenames it emits. A leading `.` will prevent the
# use of `git apply` to fix reported shellcheck issues. This is
# compatible with both macos and linux (unlike the use of -printf).
#
# shellcheck disable=SC2035
find * -name "*.sh" -type f -print0 | xargs -0 "${SHELLCHECK}" "${@}"
