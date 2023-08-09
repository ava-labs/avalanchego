#!/bin/bash
# Exits if any uncommitted changes are found.

clean=$(git status | grep "nothing to commit, working tree clean")
if [ -z "$clean" ]; then
    exit 1
fi
