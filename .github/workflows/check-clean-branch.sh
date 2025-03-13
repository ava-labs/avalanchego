#!/usr/bin/env bash

set -euo pipefail

git add --all
git update-index --really-refresh >> /dev/null

# Show the status of the working tree.
git status --short

# Exits if any uncommitted changes are found.
git diff-index --quiet HEAD
