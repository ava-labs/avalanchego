#!/bin/bash
# Exits if any uncommitted changes are found.

set -o errexit # Ensure script failure on error. Without it execution continues in most cases (excepting some explicit conditional checks).
set -o nounset # Ensures an error when an unset variable is referenced (i.e. requires explicit variable initialization).
set -o pipefail # Ensures an error when a pipe call (both with `|` and `$( )`) fails.

git update-index --really-refresh >> /dev/null
git diff-index --quiet HEAD
