#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/protobuf_codegen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

## ensure the correct version of "buf" is installed
BUF_VERSION='1.31.0'
if [[ $(buf --version | cut -f2 -d' ') != "${BUF_VERSION}" ]]; then
  echo "could not find buf ${BUF_VERSION}, is it installed + in PATH?"
  exit 255
fi

## install "protoc-gen-go"
PROTOC_GEN_GO_VERSION='v1.33.0'
go install -v google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION}
if [[ $(protoc-gen-go --version | cut -f2 -d' ') != "${PROTOC_GEN_GO_VERSION}" ]]; then
  # e.g., protoc-gen-go v1.28.1
  echo "could not find protoc-gen-go ${PROTOC_GEN_GO_VERSION}, is it installed + in PATH?"
  exit 255
fi

### install "protoc-gen-go-grpc"
PROTOC_GEN_GO_GRPC_VERSION='1.3.0'
go install -v google.golang.org/grpc/cmd/protoc-gen-go-grpc@v${PROTOC_GEN_GO_GRPC_VERSION}
if [[ $(protoc-gen-go-grpc --version | cut -f2 -d' ') != "${PROTOC_GEN_GO_GRPC_VERSION}" ]]; then
  # e.g., protoc-gen-go-grpc 1.3.0
  echo "could not find protoc-gen-go-grpc ${PROTOC_GEN_GO_GRPC_VERSION}, is it installed + in PATH?"
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
