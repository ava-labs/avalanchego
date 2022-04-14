#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

echo "Building docker image based off of most recent local commits of caminogo and caminoethvm"

CAMINO_REMOTE="git@github.com:chain4travel/caminogo.git"
CAMINOETHVM_REMOTE="git@github.com:chain4travel/caminoethvm.git"
DOCKERHUB_REPO="c4tplatform/caminogo"

DOCKER="${DOCKER:-docker}"
SCRIPT_DIRPATH=$(cd $(dirname "${BASH_SOURCE[0]}") && pwd)
ROOT_DIRPATH="$(dirname "${SCRIPT_DIRPATH}")"

C4T_RELATIVE_PATH="src/github.com/chain4travel"
EXISTING_GOPATH="$GOPATH"

export GOPATH="$SCRIPT_DIRPATH/.build_image_gopath"
WORKPREFIX="$GOPATH/src/github.com/chain4travel"

# Clone the remotes and checkout the desired branch/commits
CAMINO_CLONE="$WORKPREFIX/caminogo"
CAMINOETHVM_CLONE="$WORKPREFIX/caminoethvm"

# Replace the WORKPREFIX directory
rm -rf "$WORKPREFIX"
mkdir -p "$WORKPREFIX"


CAMINO_COMMIT_HASH="$(git -C "$EXISTING_GOPATH/$C4T_RELATIVE_PATH/caminogo" rev-parse --short HEAD)"
CAMINOETHVM_COMMIT_HASH="$(git -C "$EXISTING_GOPATH/$C4T_RELATIVE_PATH/caminoethvm" rev-parse --short HEAD)"

git config --global credential.helper cache

git clone "$CAMINO_REMOTE" "$CAMINO_CLONE"
git -C "$CAMINO_CLONE" checkout "$CAMINO_COMMIT_HASH"

git clone "$CAMINOETHVM_REMOTE" "$CAMINOETHVM_CLONE"
git -C "$CAMINOETHVM_CLONE" checkout "$CAMINOETHVM_COMMIT_HASH"

CONCATENATED_HASHES="$CAMINO_COMMIT_HASH-$CAMINOETHVM_COMMIT_HASH"

"$DOCKER" build -t "$DOCKERHUB_REPO:$CONCATENATED_HASHES" "$WORKPREFIX" -f "$SCRIPT_DIRPATH/local.Dockerfile"
