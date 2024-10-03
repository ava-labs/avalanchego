#!/bin/bash
# Exits if any uncommitted changes are found.

set -euo pipefail

git add --all
git update-index --really-refresh >> /dev/null
git diff-index --quiet HEAD
