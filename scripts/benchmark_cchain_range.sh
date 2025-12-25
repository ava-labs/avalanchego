#!/usr/bin/env bash

set -euo pipefail

# This script runs the C-Chain re-execution benchmark with a single iteration.
# It expects the following environment variables to be set:
#   BLOCK_DIR: Path or S3 URL to the block directory or zip.
#   CURRENT_STATE_DIR: Path or S3 URL to the current state directory or zip.
#   START_BLOCK: The starting block height (exclusive).
#   END_BLOCK: The ending block height (inclusive).
#   RUNNER_TYPE (optional): Runner type/label to include in benchmark naming.
#   LABELS (optional): Comma-separated key=value pairs for metric labels.
#   BENCHMARK_OUTPUT_FILE (optional): If set, benchmark output is also written to this file.
#   METRICS_SERVER_ENABLED (optional): If set, enables the metrics server.
#   METRICS_SERVER_PORT (optional): If set, determines the port the metrics server will listen to.
#   METRICS_COLLECTOR_ENABLED (optional): If set, enables the metrics collector.
#   PROFILE (optional, bool): If set, build with debug symbols and enable pprof.

BINARY="$(mktemp -d)/vm_reexecute"

if [[ "${PROFILE:-}" == "true" ]]; then
  # Build with debug symbols for profiling (pprof, perf, samply, Instruments).
  # -gcflags="all=-N -l":
  #   -N: Disable optimizations so variable values are preserved in debugger
  #   -l: Disable inlining so all function calls appear in stack traces
  # -ldflags="-compressdwarf=false":
  #   Keep DWARF debug info uncompressed so profilers can read symbols
  # CGO_CFLAGS="-fno-omit-frame-pointer -g":
  #   -fno-omit-frame-pointer: Preserve frame pointers for stack unwinding (required for profilers to walk the call stack)
  #   -g: Include debug symbols in C/FFI code (Rust FFI visibility)
  CGO_CFLAGS="-fno-omit-frame-pointer -g" \
  CGO_LDFLAGS="-g" \
  go build -o "${BINARY}" -gcflags="all=-N -l" -ldflags="-compressdwarf=false" \
    github.com/ava-labs/avalanchego/tests/reexecute/c
else
  go build -o "${BINARY}" github.com/ava-labs/avalanchego/tests/reexecute/c
fi

"${BINARY}" \
  --block-dir="${BLOCK_DIR}" \
  --current-state-dir="${CURRENT_STATE_DIR}" \
  ${RUNNER_TYPE:+--runner="${RUNNER_TYPE}"} \
  ${CONFIG:+--config="${CONFIG}"} \
  --start-block="${START_BLOCK}" \
  --end-block="${END_BLOCK}" \
  ${LABELS:+--labels="${LABELS}"} \
  ${BENCHMARK_OUTPUT_FILE:+--benchmark-output-file="${BENCHMARK_OUTPUT_FILE}"} \
  ${METRICS_SERVER_ENABLED:+--metrics-server-enabled="${METRICS_SERVER_ENABLED}"} \
  ${METRICS_SERVER_PORT:+--metrics-server-port="${METRICS_SERVER_PORT}"} \
  ${METRICS_COLLECTOR_ENABLED:+--metrics-collector-enabled="${METRICS_COLLECTOR_ENABLED}"}
#  ${PROFILE:+--pprof}
