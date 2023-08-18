#!/usr/bin/env bash

set -euo pipefail

# Archives a testnet network dir to the current working directory.

TESTNETCTL_NETWORK_DIR="${1:-}"
if [[ -z "${TESTNETCTL_NETWORK_DIR}" ]]; then
  echo "Missing TESTNETCTL_NETWORK_DIR argument!"
  echo "Usage: {0} TESTNETCTL_NETWORK_DIR" >> /dev/stderr
  exit 255
fi

AVALANCHE_PATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )"
ARCHIVE_FILENAME="${AVALANCHE_PATH}/testnet.tar.xz"
echo "Archiving network dir ${TESTNETCTL_NETWORK_DIR} to ${ARCHIVE_FILENAME}"
NETWORK_DIR="$(dirname ${TESTNETCTL_NETWORK_DIR})"
NETWORK_BASE="$(basename ${TESTNETCTL_NETWORK_DIR})"
tar cjf "${ARCHIVE_FILENAME}" -C "${NETWORK_DIR}" "${NETWORK_BASE}"
