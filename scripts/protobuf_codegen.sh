#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/protobuf_codegen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# the versions here should match those of the binaries installed in the nix dev shell

## ensure the correct version of "buf" is installed
BUF_VERSION='1.59.0'
ACTUAL_BUF_VERSION=$(buf --version 2>/dev/null | cut -f2 -d' ' || echo "not found")
if [[ "${ACTUAL_BUF_VERSION}" != "${BUF_VERSION}" ]]; then
  echo "buf version mismatch: expected ${BUF_VERSION}, got ${ACTUAL_BUF_VERSION}"
  exit 255
fi

## ensure the correct version of "protoc-gen-go" is installed
PROTOC_GEN_GO_VERSION='v1.36.10'
ACTUAL_PROTOC_GEN_GO_VERSION=$(protoc-gen-go --version 2>/dev/null | cut -f2 -d' ' || echo "not found")
if [[ "${ACTUAL_PROTOC_GEN_GO_VERSION}" != "${PROTOC_GEN_GO_VERSION}" ]]; then
  echo "protoc-gen-go version mismatch: expected ${PROTOC_GEN_GO_VERSION}, got ${ACTUAL_PROTOC_GEN_GO_VERSION}"
  exit 255
fi

## ensure the correct version of "protoc-gen-go-grpc" is installed
PROTOC_GEN_GO_GRPC_VERSION='1.5.1'
ACTUAL_PROTOC_GEN_GO_GRPC_VERSION=$(protoc-gen-go-grpc --version 2>/dev/null | cut -f2 -d' ' || echo "not found")
if [[ "${ACTUAL_PROTOC_GEN_GO_GRPC_VERSION}" != "${PROTOC_GEN_GO_GRPC_VERSION}" ]]; then
  echo "protoc-gen-go-grpc version mismatch: expected ${PROTOC_GEN_GO_GRPC_VERSION}, got ${ACTUAL_PROTOC_GEN_GO_GRPC_VERSION}"
  exit 255
fi

BUF_MODULES=("proto" "connectproto")

REPO_ROOT=$PWD
for BUF_MODULE in "${BUF_MODULES[@]}"; do
  TARGET=$REPO_ROOT/$BUF_MODULE
  if [ -n "${1:-}" ]; then
    TARGET="$1"
  fi

  # move to buf module directory
  cd "$TARGET"

  echo "Generating for buf module $BUF_MODULE"
  echo "Running protobuf fmt for..."
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
done
