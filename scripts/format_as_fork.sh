#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail
set -x

script_dir=$(dirname "$0")

commit_msg_remove_header="format: remove avalanche header"
commit_msg_remove_upstream="format: remove upstream go-ethereum"
commit_msg_rename_packages_as_fork="format: rename packages as fork"

make_commit() {
    if git diff-index --cached --quiet HEAD --; then
        echo "No changes to commit."
    else
        git commit -m "$1"
    fi
}

revert_by_message() {
    hash=$(git log --grep="$1" --format="%H" -n 1)    
    git revert --no-edit "$hash"
}

if git status --porcelain | grep -q '^ M'; then
    echo "There are edited files in the repository. Please commit or stash them before running this script."
    exit 1
fi

upstream_dirs=$(sed -e 's/"github.com\/ethereum\/go-ethereum\/\(.*\)"/\1/' "${script_dir}"/geth-allowed-packages.txt | xargs)
for dir in ${upstream_dirs}; do
    if [ -d "${dir}" ]; then
        git rm -r "${dir}"
    fi
done
git clean -df -- "${upstream_dirs}"
make_commit "${commit_msg_remove_upstream}"

sed_command='s!\([^/]\)github.com/ethereum/go-ethereum!\1github.com/ava-labs/subnet-evm!g'
find . \( -name '*.go' -o -name 'go.mod' -o -name 'build_test.sh' \) -exec sed -i '' -e "${sed_command}" {} \;
for dir in ${upstream_dirs}; do
    sed_command="s!\"github.com/ava-labs/subnet-evm/${dir}\"!\"github.com/ethereum/go-ethereum/${dir}\"!g"
    find . -name '*.go' -exec sed -i '' -e "${sed_command}" {} \;
done
go get github.com/ethereum/go-ethereum@"$1"
gofmt -w .
go mod tidy
git add -u .
make_commit "${commit_msg_rename_packages_as_fork}"

revert_by_message "${commit_msg_remove_header}"