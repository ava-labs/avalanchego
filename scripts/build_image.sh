#!/bin/bash -e
SRC_DIR="$(dirname "${BASH_SOURCE[0]}")"
export GOPATH="$SRC_DIR/.build_image_gopath"
WORKPREFIX="$GOPATH/src/github.com/ava-labs/"
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

if [[ ! -d "$WORKPREFIX" ]]; then
    mkdir -p "$WORKPREFIX"
    git config --global credential.helper cache
    git clone https://github.com/ava-labs/gecko.git "$WORKPREFIX/gecko"
fi
GECKO_COMMIT="$(git --git-dir="$WORKPREFIX/gecko/.git" rev-parse --short HEAD)"
"${DOCKER}" build -t "gecko-$GECKO_COMMIT" "$SRC_DIR" -f "$SRC_DIR/Dockerfile.deploy"
