#!/bin/bash
# Exits if any uncommitted changes are found.

set -o errexit
set -o nounset
set -o pipefail

git update-index --really-refresh >> /dev/null
git diff-index --quiet HEAD
