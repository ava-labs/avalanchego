#!/usr/bin/env bash

set -euo pipefail

# Sets up the pinned Task release binary for CI. The action calls this script to
# read the version, download Task on a cache miss, and add the extracted binary to PATH.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# The pinned Task release version.
TASK_VERSION="${TASK_VERSION:-}"

# Stores the extracted binary for the job and cache action.
RUNNER_TEMP="${RUNNER_TEMP:-}"

# Selects the Task release operating system.
RUNNER_OS="${RUNNER_OS:-}"

# Selects the Task release architecture.
RUNNER_ARCH="${RUNNER_ARCH:-}"

# CI uses the Go tool dependency as its Task version pin. sync_task_version.sh
# keeps this pin synchronized with the version supplied by Nix.
read_version() {
  local version
  version="$(awk '$1 == "github.com/go-task/task/v3" { print $2 }' "${REPO_ROOT}/tools/external/go.mod")"
  if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Could not find a pinned Task version in tools/external/go.mod" >&2
    exit 1
  fi
  echo "${version}"
}

# Cache each Task release separately for its operating system and architecture.
task_dir() {
  echo "${RUNNER_TEMP:?RUNNER_TEMP must be set}/task/${TASK_VERSION:?TASK_VERSION must be set}/${RUNNER_OS:?RUNNER_OS must be set}-${RUNNER_ARCH:?RUNNER_ARCH must be set}"
}

# These expected archive checksums are reviewed with each Task version update.
# sync_task_version.sh replaces the marked block from the release checksum file.
task_archive_checksum() {
  case "${TASK_VERSION:?TASK_VERSION must be set}:$1" in
    # BEGIN TASK ARCHIVE CHECKSUMS
    v3.48.0:task_darwin_arm64.tar.gz) echo 9a2727b7b74821e1a0a1fb17c477a7e84b7f1bbc8c3ca3bf47ed9a9557c2b986 ;;
    v3.48.0:task_linux_amd64.tar.gz) echo f4bfc4eef1b2557b262f3cc0a79976a421885cb7b9e71cfe75568a2ebe4d7ae5 ;;
    v3.48.0:task_linux_arm64.tar.gz) echo 15a54fd45f706ce0f4c679c93cf04e02d72ca7223f157209201a1576008056d6 ;;
    # END TASK ARCHIVE CHECKSUMS
    *)
      echo "No pinned checksum for Task ${TASK_VERSION} archive $1" >&2
      exit 1
      ;;
  esac
}

download_task() {
  local archive archive_name expected_checksum actual_checksum os arch release_url directory

  # Map only the GitHub Actions platforms supported by this repository to the
  # Task release archive names.
  case "${RUNNER_OS:?RUNNER_OS must be set}-${RUNNER_ARCH:?RUNNER_ARCH must be set}" in
    Linux-X64)
      os=linux
      arch=amd64
      ;;
    Linux-ARM64)
      os=linux
      arch=arm64
      ;;
    macOS-ARM64)
      os=darwin
      arch=arm64
      ;;
    *)
      echo "Unsupported Task platform: ${RUNNER_OS}-${RUNNER_ARCH}" >&2
      exit 1
      ;;
  esac

  directory="$(task_dir)"
  # A restored cache already contains the extracted binary.
  if [[ -x "${directory}/task" ]]; then
    exit 0
  fi

  archive_name="task_${os}_${arch}.tar.gz"
  release_url="https://github.com/go-task/task/releases/download/${TASK_VERSION:?TASK_VERSION must be set}"
  archive="${RUNNER_TEMP}/${archive_name}"

  curl --fail --location --retry 3 --retry-all-errors --output "${archive}" "${release_url}/${archive_name}"
  # Compare against the reviewed checksum, not a checksum file downloaded with
  # the archive.
  expected_checksum="$(task_archive_checksum "${archive_name}")"
  actual_checksum="$(shasum -a 256 "${archive}" | awk '{ print $1 }')"
  if [[ -z "${expected_checksum}" || "${actual_checksum}" != "${expected_checksum}" ]]; then
    echo "Task archive checksum verification failed" >&2
    exit 1
  fi

  mkdir -p "${directory}"
  tar -xzf "${archive}" -C "${directory}"
  test -x "${directory}/task"
}

case "${1-}" in
  version) read_version ;;
  checksum) task_archive_checksum "${2:?archive name must be set}" ;;
  download) download_task ;;
  path)
    directory="$(task_dir)"
    test -x "${directory}/task"
    echo "${directory}"
    ;;
  *)
    echo "usage: $0 {version|checksum <archive>|download|path}" >&2
    exit 2
    ;;
esac
