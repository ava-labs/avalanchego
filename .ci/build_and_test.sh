#!/usr/bin/env bash

set -ev

cd $TRAVIS_BUILD_DIR 
./scripts/build.sh
# Check to see if the build script creates any unstaged changes to prevent
# regression where builds go.mod/go.sum files get out of date.
if [[ -z $(git status -s) ]]; then
    echo "Build script created unstaged changes in the repository"
    # TODO: Revise this check once we can reliably build without changes
    # exit 1
fi
./scripts/build_test.sh
