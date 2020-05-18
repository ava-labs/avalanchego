#!/bin/bash

set -ev

go get -d -v github.com/ava-labs/gecko/...

cd $GOPATH/src/github.com/ava-labs/gecko
# ./scripts/build_test.sh
./scripts/build.sh
