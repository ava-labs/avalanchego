#!/bin/bash
# Exits if any uncommitted changes are found.

clean=$(git status | grep "nothing to commit (working directory clean)")
if [ -z "$clean" ]; then
    exit 1
fi