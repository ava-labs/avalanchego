#!/bin/bash

set -ev

bash <(curl -s https://codecov.io/bash)

TRAVIS_TAG="$DOCKERHUB_REPO:travis-$TRAVIS_BUILD_NUMBER"
docker tag $DOCKERHUB_REPO:$COMMIT "$TRAVIS_TAG"

# don't push to dockerhub if this is not being run on the main public repo
# or if it's a PR from a fork ( => secret vars not set )
if [[ $TRAVIS_REPO_SLUG != "ava-labs/gecko" || -z "$DOCKER_USERNAME"  ]]; then
  exit 0;
fi

echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
#docker push "$TRAVIS_TAG"
# following should push all tags
docker push $DOCKERHUB_REPO 
