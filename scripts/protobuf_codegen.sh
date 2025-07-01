#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/protobuf_codegen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# the versions here should match those of the binaries installed in the nix dev shell

## ensure the correct version of "buf" is installed
BUF_VERSION='1.47.2'
if [[ $(buf --version | cut -f2 -d' ') != "${BUF_VERSION}" ]]; then
  echo "could not find buf ${BUF_VERSION}, is it installed + in PATH?"
  exit 255
fi

## ensure the correct version of "protoc-gen-go" is installed
PROTOC_GEN_GO_VERSION='v1.35.1'
if [[ $(protoc-gen-go --version | cut -f2 -d' ') != "${PROTOC_GEN_GO_VERSION}" ]]; then
  echo "could not find protoc-gen-go ${PROTOC_GEN_GO_VERSION}, is it installed + in PATH?"
  exit 255
fi

## ensure the correct version of "protoc-gen-go-grpc" is installed
PROTOC_GEN_GO_GRPC_VERSION='1.3.0'
if [[ $(protoc-gen-go-grpc --version | cut -f2 -d' ') != "${PROTOC_GEN_GO_GRPC_VERSION}" ]]; then
  echo "could not find protoc-gen-go-grpc ${PROTOC_GEN_GO_GRPC_VERSION}, is it installed + in PATH?"
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
