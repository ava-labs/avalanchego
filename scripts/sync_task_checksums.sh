#!/usr/bin/env bash

# Regenerates the checked-in Task release checksums for supported CI platforms.
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <Task version> <checksum URL> <setup_task.sh path>" >&2
  exit 2
fi

# The version that keys each checked-in checksum entry.
TASK_VERSION="$1"
# The Task release checksum file used only while updating checked-in values.
CHECKSUMS_URL="$2"
# The setup script whose marked checksum block is replaced.
SETUP_TASK_PATH="$3"

if ! [[ "${TASK_VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "invalid Task version: ${TASK_VERSION}" >&2
  exit 1
fi
if [[ ! -f "${SETUP_TASK_PATH}" ]]; then
  echo "setup Task script not found: ${SETUP_TASK_PATH}" >&2
  exit 1
fi
if [[ $(grep -c '^    # BEGIN TASK ARCHIVE CHECKSUMS$' "${SETUP_TASK_PATH}") -ne 1 ]] ||
  [[ $(grep -c '^    # END TASK ARCHIVE CHECKSUMS$' "${SETUP_TASK_PATH}") -ne 1 ]]; then
  echo "setup Task script must contain one checksum block" >&2
  exit 1
fi

CHECKSUM_FILE="$(mktemp)"
UPDATED_SCRIPT="$(mktemp)"
trap 'rm -f "${CHECKSUM_FILE}" "${UPDATED_SCRIPT}"' EXIT

curl --fail --location --retry 3 --retry-all-errors --output "${CHECKSUM_FILE}" "${CHECKSUMS_URL}"
CHECKSUM_CASES="$(awk -v version="${TASK_VERSION}" '
  $1 ~ /^[[:xdigit:]]{64}$/ && $2 ~ /^(task_darwin_arm64|task_linux_amd64|task_linux_arm64)\.tar\.gz$/ {
    printf "    %s:%s) echo %s ;;\n", version, $2, $1
    count++
  }
  END {
    if (count != 3) {
      exit 1
    }
  }
' "${CHECKSUM_FILE}")" || {
  echo "Task release checksums did not include all supported archives" >&2
  exit 1
}

awk -v checksums="${CHECKSUM_CASES}" '
  /# BEGIN TASK ARCHIVE CHECKSUMS/ {
    print
    print checksums
    replacing = 1
    next
  }
  /# END TASK ARCHIVE CHECKSUMS/ {
    replacing = 0
  }
  !replacing { print }
' "${SETUP_TASK_PATH}" >"${UPDATED_SCRIPT}"
chmod +x "${UPDATED_SCRIPT}"
mv "${UPDATED_SCRIPT}" "${SETUP_TASK_PATH}"
