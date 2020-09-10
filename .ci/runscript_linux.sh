#!/usr/bin/env bash

set -ev

docker run --rm -v "$PWD:$AVALANCHE_HOME" $DOCKERHUB_REPO:$COMMIT bash "$AVALANCHE_HOME/scripts/build_test.sh"
docker run --rm -v "$PWD:$AVALANCHE_HOME" $DOCKERHUB_REPO:$COMMIT bash "$AVALANCHE_HOME/scripts/build.sh"
