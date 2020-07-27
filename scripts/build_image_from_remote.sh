#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail


SRC_DIR="$(dirname "${BASH_SOURCE[0]}")"
BASE_DIR="$SRC_DIR/.."

# Two arguments specify the remote and branch to use
REMOTE=$1
BRANCH=$2


echo "Building docker image from branch: $BRANCH at remote: $REMOTE"

export GOPATH="$SRC_DIR/.build_image_gopath"
WORKPREFIX="$GOPATH/src/github.com/ava-labs"
DOCKER="${DOCKER:-docker}"
keep_existing=0
while getopts 'k' opt
do
    case $opt in
    (k) keep_existing=1;;
    esac
done
if [[ "$keep_existing" != 1 ]]; then
    rm -rf "$WORKPREFIX"
fi

# Clone the remote and checkout the specified branch to build the Docker image from
GECKO_CLONE="$WORKPREFIX/gecko"

if [[ ! -d "$WORKPREFIX" ]]; then
    mkdir -p "$WORKPREFIX"
    git config --global credential.helper cache
    git clone "$REMOTE" "$GECKO_CLONE"
    git --git-dir="$GECKO_CLONE/.git" checkout "$BRANCH"
fi

GECKO_COMMIT="$(git --git-dir="$GECKO_CLONE/.git" rev-parse --short HEAD)"

"${DOCKER}" build -t "gecko-$GECKO_COMMIT" "$GECKO_CLONE" -f "$GECKO_CLONE/Dockerfile"
