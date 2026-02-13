#!/usr/bin/env bash

# First argument is the time, in seconds, to run each fuzz test for.
# If not provided, defaults to 1 second.
#
# Second argument is the directory to run fuzz tests in.
# If not provided, defaults to the current directory.

set -euo pipefail

# Mostly taken from https://github.com/golang/go/issues/46312#issuecomment-1153345129

# Directory above this script
AVALANCHE_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Load the constants
source "$AVALANCHE_PATH"/scripts/constants.sh

fuzzTime=${1:-1}
fuzzDir=${2:-.}

# Set go test timeout to fuzz time + 20 minutes to allow for compilation and setup
timeout=$((fuzzTime + 1200))

EXCLUDE_DIR="graft"

files=$(grep -r --exclude-dir="$EXCLUDE_DIR" --include='**_test.go' --files-with-matches 'func Fuzz' "$fuzzDir")
failed=false
for file in ${files}
do
    funcs=$(grep -oP 'func \K(Fuzz\w*)' "$file")
    for func in ${funcs}
    do
        echo "Fuzzing $func in $file"
        parentDir=$(dirname "$file")
        # If any of the fuzz tests fail, return exit code 1
        if ! go test -tags test -timeout="${timeout}s" "$parentDir" -run="$func" -fuzz="$func" -fuzztime="${fuzzTime}"s; then
            failed=true
        fi
    done
done

if $failed; then
    exit 1
fi
