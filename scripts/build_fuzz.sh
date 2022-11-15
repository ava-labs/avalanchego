#!/usr/bin/env bash

# Mostly taken from https://github.com/golang/go/issues/46312#issuecomment-1153345129

fuzzTime=${1:-1}
files=$(grep -r --include='**_test.go' --files-with-matches 'func Fuzz' .)
failed=false
for file in ${files}
do
    funcs=$(grep -oP 'func \K(Fuzz\w*)' $file)
    for func in ${funcs}
    do
        echo "Fuzzing $func in $file"
        parentDir=$(dirname $file)
        go test $parentDir -run=$func -fuzz=$func -fuzztime=${fuzzTime}s
        # If any of the fuzz tests fail, return exit code 1
        if [ $? -ne 0 ]; then
            failed=true
        fi
    done
done

if $failed; then
    exit 1
fi
