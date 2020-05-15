#!/bin/bash

set -ev

cd $GOPATH/src/github.com/ava-labs/gecko
./scripts/build_test.sh
./scripts/build.sh
