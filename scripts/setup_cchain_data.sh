#!/usr/bin/env bash

set -euo pipefail

# Set up C-Chain test data
#
# Downloads blocks from S3 and sets up state directory. For genesis execution,
# omit CURRENT_STATE_DIR_SRC to create an empty state directory.
#
# Required env vars:
#   BLOCK_DIR_SRC         - S3 object key for blocks (e.g., cchain-mainnet-blocks-200-ldb)
#   EXECUTION_DATA_DIR    - Local directory to store imported data
#
# Optional env vars:
#   CURRENT_STATE_DIR_SRC - S3 object key for state (e.g., cchain-current-state-hashdb-full-100)
#                           If not set, creates an empty current-state directory (genesis mode).
#
# Result:
#   Creates $EXECUTION_DATA_DIR/blocks and $EXECUTION_DATA_DIR/current-state

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
S3_BOOTSTRAP_BUCKET="${S3_BOOTSTRAP_BUCKET:-s3://avalanchego-bootstrap-testing}"

: "${BLOCK_DIR_SRC:?BLOCK_DIR_SRC must be set}"
: "${EXECUTION_DATA_DIR:?EXECUTION_DATA_DIR must be set}"

echo "=== Importing blocks from S3 ==="
"${SCRIPT_DIR}/copy_dir.sh" "${S3_BOOTSTRAP_BUCKET}/${BLOCK_DIR_SRC}/**" "${EXECUTION_DATA_DIR}/blocks"

if [[ -n "${CURRENT_STATE_DIR_SRC:-}" ]]; then
    echo "=== Importing state from S3 ==="
    "${SCRIPT_DIR}/copy_dir.sh" "${S3_BOOTSTRAP_BUCKET}/${CURRENT_STATE_DIR_SRC}/**" "${EXECUTION_DATA_DIR}/current-state"
else
    echo "=== Genesis mode: creating empty state directory ==="
    mkdir -p "${EXECUTION_DATA_DIR}/current-state"
fi
