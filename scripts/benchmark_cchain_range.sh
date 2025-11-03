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
#   METRICS_SERVER_ENABLED (optional): If set, enables the metrics server.
#   METRICS_SERVER_PORT (optional): If set, determines the port the metrics server will listen to.
#   METRICS_COLLECTOR_ENABLED (optional): If set, enables the metrics collector.

: "${BLOCK_DIR:?BLOCK_DIR must be set}"
: "${CURRENT_STATE_DIR:?CURRENT_STATE_DIR must be set}"
: "${START_BLOCK:?START_BLOCK must be set}"
: "${END_BLOCK:?END_BLOCK must be set}"

# Defaults to "dev" set at vm_reexecute_test.go `runnerNameArg`
: "${RUNNER_NAME:=dev}"

cmd="go test -timeout=0 -v -benchtime=1x -bench=BenchmarkReexecuteRange -run=^$ github.com/ava-labs/avalanchego/tests/reexecute/c \
  --block-dir=\"${BLOCK_DIR}\" \
  --current-state-dir=\"${CURRENT_STATE_DIR}\" \
  --runner=\"${RUNNER_NAME}\" \
  ${CONFIG:+--config=\"${CONFIG}\"} \
  --start-block=\"${START_BLOCK}\" \
  --end-block=\"${END_BLOCK}\" \
  ${LABELS:+--labels=\"${LABELS}\"} \
  ${METRICS_SERVER_ENABLED:+--metrics-server-enabled=\"${METRICS_SERVER_ENABLED}\"} \
  ${METRICS_SERVER_PORT:+--metrics-server-port=\"${METRICS_SERVER_PORT}\"} \
  ${METRICS_COLLECTOR_ENABLED:+--metrics-collector-enabled=\"${METRICS_COLLECTOR_ENABLED}\"}"

if [ -n "${BENCHMARK_OUTPUT_FILE:-}" ]; then
  eval "$cmd" | tee "${BENCHMARK_OUTPUT_FILE}"
else
  eval "$cmd"
fi
