#!/usr/bin/env bash

set -euo pipefail

# This script runs the C-Chain re-execution benchmark with a single iteration.
# It expects the following environment variables to be set:
#   BLOCK_DIR: Path or S3 URL to the block directory or zip.
#   CURRENT_STATE_DIR: Path or S3 URL to the current state directory or zip.
#   START_BLOCK: The starting block height (exclusive).
#   END_BLOCK: The ending block height (inclusive).
#   LABELS (optional): Comma-separated key=value pairs for metric labels.
#   BENCHMARK_OUTPUT_FILE (optional): If set, benchmark output is also written to this file.

: "${BLOCK_DIR:?BLOCK_DIR must be set}"
: "${CURRENT_STATE_DIR:?CURRENT_STATE_DIR must be set}"
: "${RUNNER_NAME:?RUNNER_NAME must be set}"
: "${START_BLOCK:?START_BLOCK must be set}"
: "${END_BLOCK:?END_BLOCK must be set}"

cmd="go test -timeout=0 -v -benchtime=1x -bench=BenchmarkReexecuteRange -run=^$ github.com/ava-labs/avalanchego/tests/reexecute/c \
  --block-dir=\"${BLOCK_DIR}\" \
  --current-state-dir=\"${CURRENT_STATE_DIR}\" \
  --runner=\"${RUNNER_NAME}\" \
  ${CONFIG:+--config=\"${CONFIG}\"} \
  --start-block=\"${START_BLOCK}\" \
  --end-block=\"${END_BLOCK}\" \
  ${LABELS:+--labels=\"${LABELS}\"} \
  ${METRICS_MODE:+--metrics-mode=\"${METRICS_MODE}\"}"

if [ -n "${BENCHMARK_OUTPUT_FILE:-}" ]; then
  eval "$cmd" | tee "${BENCHMARK_OUTPUT_FILE}"
else
  eval "$cmd"
fi
