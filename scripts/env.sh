#!/bin/bash -e

export CORETH_VER="v0.1.0"     # Must match coreth version in go.mod
export CORETH_PATH=$GOPATH/pkg/mod/github.com/ava-labs/coreth@$CORETH_VER

export SALTICIDAE_VER="v0.3.0" # Must match salticidae version in go.mod
export SALTICIDAE_PATH=$GOPATH/pkg/mod/github.com/ava-labs/salticidae@$SALTICIDAE_VER

export BUILD_DIR="${GECKO_PATH}/build" # Where binaries go