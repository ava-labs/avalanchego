#!/usr/bin/env bash

set -euo pipefail

# Import C-Chain blocks and state from S3
#
# Required env vars:
#   BLOCK_DIR_SRC         - S3 object key for blocks (e.g., cchain-mainnet-blocks-200-ldb)
#   CURRENT_STATE_DIR_SRC - S3 object key for state (e.g., cchain-current-state-hashdb-full-100)
#   EXECUTION_DATA_DIR    - Local directory to store imported data
#
# Result:
#   Creates $EXECUTION_DATA_DIR/blocks and $EXECUTION_DATA_DIR/current-state

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
S3_BOOTSTRAP_BUCKET="${S3_BOOTSTRAP_BUCKET:-s3://avalanchego-bootstrap-testing}"

: "${BLOCK_DIR_SRC:?BLOCK_DIR_SRC must be set}"
: "${CURRENT_STATE_DIR_SRC:?CURRENT_STATE_DIR_SRC must be set}"
: "${EXECUTION_DATA_DIR:?EXECUTION_DATA_DIR must be set}"

echo "=== Importing blocks from S3 ==="
"${SCRIPT_DIR}/copy_dir.sh" "${S3_BOOTSTRAP_BUCKET}/${BLOCK_DIR_SRC}/**" "${EXECUTION_DATA_DIR}/blocks"

echo "=== Importing state from S3 ==="
"${SCRIPT_DIR}/copy_dir.sh" "${S3_BOOTSTRAP_BUCKET}/${CURRENT_STATE_DIR_SRC}/**" "${EXECUTION_DATA_DIR}/current-state"
