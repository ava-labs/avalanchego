#!/usr/bin/env bash

set -euo pipefail

# C-Chain Range Checkpointing Script
#
# This script processes sequential ranges of blocks for C-Chain checkpointing.
# It reads configuration from environment variables and creates checkpoints
# at specified end blocks.
#
# Required Re-Execution Environment Variables:
#   START_BLOCK         - Starting block number (e.g., 1)
#   CURRENT_STATE_DIR   - Path to current state directory (e.g., "/mnt/ssd1/current-state")
#   SOURCE_BLOCK_DIR    - Path to source block directory (e.g., "/mnt/ssd1/blocks")
#   CHECKPOINTS         - Comma-separated list of checkpoint blocks (e.g., "10,20,30,40,50" or "1k,5k,10m,20m")
#
# Required S3 Environment Variable:
#   S3_DST_PREFIX - S3 bucket/object prefix for S3 checkpoints.
#   End height of each checkpoint will be added to this suffix
#   Ex. S3_DST_PREFIX="s3://avalanchego-bootstrap-testing/cchain-current-state-firewood"
#   will create checkpoints at:
#       s3://avalanchego-bootstrap-testing/cchain-current-state-firewood-10/
#       s3://avalanchego-bootstrap-testing/cchain-current-state-firewood-20/
#       s3://avalanchego-bootstrap-testing/cchain-current-state-firewood-30/
#       s3://avalanchego-bootstrap-testing/cchain-current-state-firewood-40/
#       s3://avalanchego-bootstrap-testing/cchain-current-state-firewood-50/
#
# See reexecute-cchain-range task in [Taskfile.yml](./../Taskfile.yml) for
# all optional variables to configure re-execution itself.
#
# Example Usage:
#   export START_BLOCK=1
#   export CURRENT_STATE_DIR=/mnt/ssd1/current-state
#   export SOURCE_BLOCK_DIR=/mnt/ssd1/blocks
#   export CHECKPOINTS="1k,2k,3k"
#   export S3_DST_PREFIX="s3://avalanchego-bootstrap-testing/cchain-current-state-firewood"
#   export CONFIG=firewood
#   ./checkpoint_cchain_range.sh

# Validate required environment variables
: "${START_BLOCK:?START_BLOCK must be set}"
: "${CURRENT_STATE_DIR:?CURRENT_STATE_DIR must be set}"
: "${SOURCE_BLOCK_DIR:?SOURCE_BLOCK_DIR must be set}"
: "${CHECKPOINTS:?CHECKPOINTS must be set}"
: "${S3_DST_PREFIX:?S3_DST_PREFIX must be set}"

# Function to parse number with k/m suffixes and convert to actual number
parse_number() {
    local value="$1"
    if [[ "$value" =~ ^([0-9]+)k$ ]]; then
        echo $((${BASH_REMATCH[1]} * 1000))
    elif [[ "$value" =~ ^([0-9]+)m$ ]]; then
        echo $((${BASH_REMATCH[1]} * 1000000))
    else
        echo "$value"
    fi
}

# Function to convert number to k/m suffix for S3 path
format_for_s3() {
    local number="$1"
    if [ "$number" -ge 1000000 ] && [ $((number % 1000000)) -eq 0 ]; then
        echo "$((number / 1000000))m"
    elif [ "$number" -ge 1000 ] && [ $((number % 1000)) -eq 0 ]; then
        echo "$((number / 1000))k"
    else
        echo "$number"
    fi
}

# Convert CHECKPOINTS comma-separated string to array
IFS=',' read -ra CHECKPOINT_BLOCKS <<< "$CHECKPOINTS"

if [ ${#CHECKPOINT_BLOCKS[@]} -eq 0 ]; then
    echo "Error: At least one checkpoint block must be provided in CHECKPOINTS"
    exit 1
fi

current_start="$START_BLOCK"

for checkpoint in "${CHECKPOINT_BLOCKS[@]}"; do
    # Parse the checkpoint value (handles k/m suffixes)
    end_block=$(parse_number "$checkpoint")
    
    # Format for S3 path (converts to k/m suffixes when appropriate)
    s3_suffix=$(format_for_s3 "$end_block")
    
    echo "Processing range: $current_start to $end_block"
    
    task reexecute-cchain-range START_BLOCK="$current_start" END_BLOCK="$end_block"
    task export-dir-to-s3 LOCAL_SRC="${CURRENT_STATE_DIR}/" S3_DST="${S3_DST_PREFIX}-${s3_suffix}/"
    
    # Next start block is current end block + 1
    current_start=$((end_block + 1))
done