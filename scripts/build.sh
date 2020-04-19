#!/bin/bash -e

# Ted: contact me when you make any changes

PREFIX="${PREFIX:-$(pwd)/build}"
PLUGIN_PREFIX="$PREFIX/plugins"

SRC_DIR="$(dirname "${BASH_SOURCE[0]}")"
source "$SRC_DIR/env.sh"

CORETH_PKG=github.com/ava-labs/coreth
CORETH_PATH="$GOPATH/src/$CORETH_PKG"
if [[ -d "$CORETH_PATH/.git" ]]; then
    cd "$CORETH_PATH"
    go get -t -v -d "./..."
    cd -
else
    go get -t -v -d "$CORETH_PKG/..."
fi
cd "$CORETH_PATH"
git branch tags/v0.1.0
cd -

GECKO_PKG=github.com/ava-labs/gecko
GECKO_PATH="$GOPATH/src/$GECKO_PKG"
if [[ -d "$GECKO_PATH/.git" ]]; then
    cd "$GECKO_PATH"
    go get -t -v -d "./..."
    cd -
else
    go get -t -v -d "$GECKO_PKG/..."
fi

go build -o "$PREFIX/ava" "$GECKO_PATH/main/"*.go
go build -o "$PREFIX/xputtest" "$GECKO_PATH/xputtest/"*.go
go build -o "$PLUGIN_PREFIX/evm" "$CORETH_PATH/plugin/"*.go
