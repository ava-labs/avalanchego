#!/bin/bash

if ! [[ "$0" =~ scripts/protobuf_codegen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

## install "buf"
# any version changes here should also be bumped in Dockerfile.buf
# ref. https://docs.buf.build/installation
# ref. https://github.com/bufbuild/buf/releases
BUF_VERSION='1.11.0'
if [[ $(buf --version | cut -f2 -d' ') != "${BUF_VERSION}" ]]; then
  echo "could not find buf ${BUF_VERSION}, is it installed + in PATH?"
  exit 255
fi

## install "protoc-gen-go"
# any version changes here should also be bumped in Dockerfile.buf
# ref. https://github.com/protocolbuffers/protobuf-go/releases
PROTOC_GEN_GO_VERSION='v1.28.1'
go install -v google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION}
if [[ $(protoc-gen-go --version | cut -f2 -d' ') != "${PROTOC_GEN_GO_VERSION}" ]]; then
  # e.g., protoc-gen-go v1.28.1
  echo "could not find protoc-gen-go ${PROTOC_GEN_GO_VERSION}, is it installed + in PATH?"
  exit 255
fi

### install "protoc-gen-go-grpc"
# any version changes here should also be bumped in Dockerfile.buf
# ref. https://pkg.go.dev/google.golang.org/grpc/cmd/protoc-gen-go-grpc
# ref. https://github.com/grpc/grpc-go/blob/master/cmd/protoc-gen-go-grpc/main.go
PROTOC_GEN_GO_GRPC_VERSION='1.2.0'
go install -v google.golang.org/grpc/cmd/protoc-gen-go-grpc@v${PROTOC_GEN_GO_GRPC_VERSION}
if [[ $(protoc-gen-go-grpc --version | cut -f2 -d' ') != "${PROTOC_GEN_GO_GRPC_VERSION}" ]]; then
  # e.g., protoc-gen-go-grpc 1.2.0
  echo "could not find protoc-gen-go-grpc ${PROTOC_GEN_GO_GRPC_VERSION}, is it installed + in PATH?"
  exit 255
fi

TARGET=$PWD/proto
if [ -n "$1" ]; then 
  TARGET="$1"
fi

# move to api directory
cd $TARGET

echo "Running protobuf fmt..."
buf format -w

echo "Running protobuf lint check..."
buf lint

if [[ $? -ne 0 ]];  then
    echo "ERROR: protobuf linter failed"
    exit 1
fi

echo "Re-generating protobuf..."
buf generate

if [[ $? -ne 0 ]];  then
    echo "ERROR: protobuf generation failed"
    exit 1
fi
