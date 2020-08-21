#!/usr/bin/env bash

set -ev

# Note: On OSX, GOPATH is set before GECKO_HOME environment variable
# which leads to $GOPATH within GECKO_HOME to be empty.
cd "$GOPATH/$GECKO_HOME"
./scripts/build.sh
./scripts/build_test.sh
