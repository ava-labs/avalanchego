#!/bin/bash

set -ev

cd $TRAVIS_BUILD_DIR 
./scripts/build.sh
./scripts/build_test.sh
