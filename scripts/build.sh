#!/bin/bash -e

# Ted: contact me when you make any changes

PREFIX="${PREFIX:-$(pwd)/build}"

SRC_DIR="$(dirname "${BASH_SOURCE[0]}")"
source "$SRC_DIR/env.sh"

GECKO_PKG=github.com/ava-labs/gecko
GECKO_PATH="$GOPATH/src/$GECKO_PKG"
if [[ -d "$GECKO_PATH/.git" ]]; then
    cd "$GECKO_PATH"
    go get -t -v "./..."
    cd -
else
    go get -t -v "$GECKO_PKG/..."
fi
go build -o "$PREFIX/ava" "$GECKO_PATH/main/"*.go
go build -o "$PREFIX/xputtest" "$GECKO_PATH/xputtest/"*.go
