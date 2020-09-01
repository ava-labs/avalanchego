#!/bin/bash

.ci/before_install_linux.sh

if [[ $TRAVIS_BRANCH == "master" ]]; then
  DOCKER_BUILDKIT=1 docker build --progress plain -t $DOCKERHUB_REPO:$COMMIT -t $DOCKERHUB_REPO:latest . 
else
  DOCKER_BUILDKIT=1 docker build --progress plain -t $DOCKERHUB_REPO:$COMMIT . 
fi

docker run --rm -v "$PWD:$GECKO_HOME" $DOCKERHUB_REPO:$COMMIT bash "$GECKO_HOME/scripts/build_test.sh"
docker run --rm -v "$PWD:$GECKO_HOME" $DOCKERHUB_REPO:$COMMIT bash "$GECKO_HOME/scripts/build.sh"

