#!/usr/bin/env bash

# First argument is the time, in seconds, to run each fuzz test for.
# If not provided, defaults to 1 second.
#
# Remaining arguments are the directories to run fuzz tests in.
# If not provided, defaults to the current directory.

set -euo pipefail

# Mostly taken from https://github.com/golang/go/issues/46312#issuecomment-1153345129

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

fuzzTime=${1:-1}
fuzzDirs=("${@:2}")
if (( ${#fuzzDirs[@]} == 0 )); then
    fuzzDirs=(.)
fi

# Set go test timeout to fuzz time + 20 minutes to allow for compilation and setup.
# A negative fuzz time (e.g. -1) means run until failure, so disable the timeout.
if (( fuzzTime < 0 )); then
    timeout=0
else
    timeout=$((fuzzTime + 1200))
fi

grepStatus=0
files=$(grep -r --include='*_test.go' --files-with-matches 'func Fuzz' "${fuzzDirs[@]}") || grepStatus=$?
if (( grepStatus == 1 )); then
    echo "No fuzz tests found in: ${fuzzDirs[*]}"
    exit 0
elif (( grepStatus != 0 )); then
    exit "$grepStatus"
fi

failed=false
while IFS= read -r file
do
    while IFS= read -r func
    do
        echo "Fuzzing $func in $file"
        parentDir=$(dirname "$file")
        # cd into parentDir so packages in sub-modules (e.g. ./graft/coreth)
        # resolve against their own go.mod rather than the main module.
        # If any of the fuzz tests fail, return exit code 1
        if ! ( cd "$parentDir" && go test -tags test -timeout="${timeout}s" . -run="$func" -fuzz="$func" -fuzztime="${fuzzTime}"s ); then
            failed=true
        fi
    done < <(grep -oP 'func \K(Fuzz\w*)' "$file")
done <<< "$files"

if $failed; then
    exit 1
fi
