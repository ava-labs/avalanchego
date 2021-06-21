#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail


SRC_DIR="$(dirname "${BASH_SOURCE[0]}")"


if [[ $# -eq 0 ]]; then
    source "$SRC_DIR/build_local_image.sh"
elif [[ $# -eq 2 ]]; then
    "$SRC_DIR/build_image_from_remote.sh" $@
else
    echo "Build image requires either no arguments to build from local source or two arguments to specify a remote and branch."
fi
