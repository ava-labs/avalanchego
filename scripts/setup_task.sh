#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

read_version() {
  local version
  version="$(awk '$1 == "github.com/go-task/task/v3" { print $2 }' "${repo_root}/tools/external/go.mod")"
  if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Could not find a pinned Task version in tools/external/go.mod" >&2
    exit 1
  fi
  echo "${version}"
}

task_dir() {
  echo "${RUNNER_TEMP:?RUNNER_TEMP must be set}/task/${TASK_VERSION:?TASK_VERSION must be set}/${RUNNER_OS:?RUNNER_OS must be set}-${RUNNER_ARCH:?RUNNER_ARCH must be set}"
}

download_task() {
  local arch archive archive_name checksums expected_checksum actual_checksum os release_url directory

  case "${RUNNER_OS:?RUNNER_OS must be set}" in
    Linux) os=linux ;;
    macOS) os=darwin ;;
    *)
      echo "Unsupported Task platform: ${RUNNER_OS}" >&2
      exit 1
      ;;
  esac
  case "${RUNNER_ARCH:?RUNNER_ARCH must be set}" in
    X64) arch=amd64 ;;
    ARM64) arch=arm64 ;;
    *)
      echo "Unsupported Task architecture: ${RUNNER_ARCH}" >&2
      exit 1
      ;;
  esac

  directory="$(task_dir)"
  if [[ -x "${directory}/task" ]]; then
    exit 0
  fi

  archive_name="task_${os}_${arch}.tar.gz"
  release_url="https://github.com/go-task/task/releases/download/${TASK_VERSION:?TASK_VERSION must be set}"
  archive="${RUNNER_TEMP}/${archive_name}"
  checksums="${RUNNER_TEMP}/task_checksums.txt"

  curl --fail --location --retry 3 --retry-all-errors --output "${archive}" "${release_url}/${archive_name}"
  curl --fail --location --retry 3 --retry-all-errors --output "${checksums}" "${release_url}/task_checksums.txt"
  expected_checksum="$(awk -v archive="${archive_name}" '$2 == archive { print $1 }' "${checksums}")"
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
  download) download_task ;;
  path)
    directory="$(task_dir)"
    test -x "${directory}/task"
    echo "${directory}"
    ;;
  *)
    echo "usage: $0 {version|download|path}" >&2
    exit 2
    ;;
esac
