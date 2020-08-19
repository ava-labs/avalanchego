#!/bin/bash

set -ev

go get -d -t -v github.com/ava-labs/gecko/...

cd $GECKO_HOME
./scripts/build_test.sh
./scripts/build.sh
