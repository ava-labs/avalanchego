#!/usr/bin/env bash

set -euo pipefail

git add --all
git update-index --really-refresh >> /dev/null

# Exits if any uncommitted changes are found.
git diff-index --quiet HEAD
