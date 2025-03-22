#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/protobuf_codegen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

TARGET=$PWD/proto
if [ -n "${1:-}" ]; then
  TARGET="$1"
fi

# move to api directory
cd "$TARGET"

echo "Running protobuf fmt..."
buf format -w

echo "Running protobuf lint check..."
if ! buf lint;  then
    echo "ERROR: protobuf linter failed"
    exit 1
fi

echo "Re-generating protobuf..."
if ! buf generate;  then
    echo "ERROR: protobuf generation failed"
    exit 1
fi
