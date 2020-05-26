#!/bin/bash

set -ev

docker run --rm -v "$PWD:$GECKO_HOME" $DOCKERHUB_REPO:$COMMIT bash "$GECKO_HOME/scripts/build_test.sh"
docker run --rm -v "$PWD:$GECKO_HOME" $DOCKERHUB_REPO:$COMMIT bash "$GECKO_HOME/scripts/build.sh"
