#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Note: this script will build a docker image by cloning a remote version of gecko into a temporary
# location and using that version's Dockerfile to build the image.

SRC_DIR="$(dirname "${BASH_SOURCE[0]}")"

REMOTE="https://github.com/ava-labs/gecko.git"
BRANCH="master"

if [[ $# -eq 2 ]]; then
    REMOTE=$1
    BRANCH=$2
else
    echo "Using default remote: $REMOTE and branch: $BRANCH to build image"
fi


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
