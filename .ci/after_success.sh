#!/bin/bash

set -ev

bash <(curl -s https://codecov.io/bash)

docker tag $DOCKERHUB_REPO:$COMMIT $DOCKERHUB_REPO:travis-$TRAVIS_BUILD_NUMBER

if [ "${TRAVIS_EVENT_TYPE}" == "push" ] && [ "${TRAVIS_BRANCH}" == "platform" ]; then
    docker tag $DOCKERHUB_REPO:$COMMIT $DOCKERHUB_REPO:$TRAVIS_BRANCH
fi

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
docker push $DOCKERHUB_REPO
