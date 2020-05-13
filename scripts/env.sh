#!/bin/bash -e

GECKO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

export CORETH_VER="v0.1.0"     # Must match coreth version in go.mod
export CORETH_PATH=$GOPATH/pkg/mod/github.com/ava-labs/coreth@$CORETH_VER

export SALTICIDAE_GO_VER="v0.1.2" # Must match salticidae version in go.mod
export SALTICIDAE_GO_PATH=$GOPATH/pkg/mod/github.com/ava-labs/salticidae-go@$SALTICIDAE_GO_VER
source $SALTICIDAE_GO_PATH/scripts/env.sh

export BUILD_DIR="${GECKO_PATH}/build" # Where binaries go
