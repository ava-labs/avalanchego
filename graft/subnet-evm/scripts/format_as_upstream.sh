#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail
set -x

script_dir=$(dirname "$0")

commit_msg_remove_header="format: remove avalanche header"
commit_msg_add_upstream="format: add upstream go-ethereum"
commit_msg_rename_packages_to_upstream="format: rename packages to upstream"

make_commit() {
    if git diff-index --cached --quiet HEAD --; then
        echo "No changes to commit."
    else
        git commit -m "$1"
    fi
}

if git status --porcelain | grep -q '^ M'; then
    echo "There are edited files in the repository. Please commit or stash them before running this script."
    exit 1
fi

sed_command='/\/\/ (c) [0-9]*\(-[0-9]*\)\{0,1\}, Ava Labs, Inc.$/,+9d'
find . -name '*.go' -exec sed -i '' -e "${sed_command}" {} \;
git add -u .
make_commit "${commit_msg_remove_header}"

upstream_tag=$(grep -o 'github.com/ethereum/go-ethereum v.*' go.mod | cut -f2 -d' ')
upstream_dirs=$(sed -e 's/"github.com\/ethereum\/go-ethereum\/\(.*\)"/\1/' "${script_dir}"/geth-allowed-packages.txt  | xargs)
upstream_dirs_array=()
IFS=" " read -r -a upstream_dirs_array <<< "$upstream_dirs"

git clean -f "${upstream_dirs_array[@]}"
git checkout "${upstream_tag}" -- "${upstream_dirs_array[@]}"
git add "${upstream_dirs_array[@]}"
make_commit "${commit_msg_add_upstream}"

sed_command='s!\([^/]\)github.com/ava-labs/subnet-evm!\1github.com/ethereum/go-ethereum!g'
find . \( -name '*.go' -o -name 'go.mod' -o -name 'build_test.sh' \) -exec sed -i '' -e "${sed_command}" {} \;
gofmt -w .
go mod tidy
git add -u .
make_commit "${commit_msg_rename_packages_to_upstream}"