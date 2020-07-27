#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

# Build image from current local source
SRC_DIR="$(dirname "${BASH_SOURCE[0]}")"
BASE_DIR="$SRC_DIR/.."

# WARNING: this will use the most recent commit even if there are un-committed changes present
COMMIT_HASH="$(git --git-dir="$BASE_DIR/.git" rev-parse --short HEAD)"
echo "Building Docker image based off of local repo at commit $COMMIT_HASH"

docker build -t "gecko-$COMMIT_HASH" "$BASE_DIR" -f "$BASE_DIR/Dockerfile"
