#!/usr/bin/env bash

set -ev

bash <(curl -s https://codecov.io/bash)

TRAVIS_TAG="$DOCKERHUB_REPO:travis-$TRAVIS_BUILD_NUMBER"
docker tag $DOCKERHUB_REPO:$COMMIT "$TRAVIS_TAG"

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
docker push "$TRAVIS_TAG"
