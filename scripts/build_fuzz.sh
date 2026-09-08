#!/usr/bin/env bash

# First argument is the number of executions to fuzz each target for. Use 0 to
# fuzz until a failure. Defaults to 1.
#
# Remaining arguments are the directories to run fuzz tests in.
# If not provided, defaults to the current directory.
#
# TODO(JonathanOppenheimer): when we update to go v1.27, change the budget back
# to durations. The bug that resulted in durations leading to flakes (false
# failures reported) was fixed by https://go-review.googlesource.com/c/go/+/774140.
# See golang/go#75804.

set -euo pipefail

# Mostly taken from https://github.com/golang/go/issues/46312#issuecomment-1153345129

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

fuzzExecs=${1:-1}
fuzzDirs=("${@:2}")
if (( ${#fuzzDirs[@]} == 0 )); then
    fuzzDirs=(.)
fi

fuzzTimeArgs=()
if (( fuzzExecs > 0 )); then
    fuzzTimeArgs=(-fuzztime="${fuzzExecs}x")
fi

# A count budget has no time limit, so cap each target. Set FUZZ_TIMEOUT=0 to
# disable, which fuzzing until a failure requires.
timeout=${FUZZ_TIMEOUT:-20m}

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
        if ! ( cd "$parentDir" && go test -timeout="$timeout" . -run="$func" -fuzz="$func" "${fuzzTimeArgs[@]}" ); then
            failed=true
        fi
    done < <(grep -oP 'func \K(Fuzz\w*)' "$file")
done <<< "$files"

if $failed; then
    exit 1
fi
