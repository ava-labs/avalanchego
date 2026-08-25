#!/usr/bin/env bash

set -euo pipefail

git add --all
git update-index --really-refresh >> /dev/null

# Show the status of the working tree.
git status --short

# Exits if any uncommitted changes are found.
if git diff-index --quiet HEAD; then
  exit 0
fi

if [[ $# -gt 0 ]]; then
  echo "Generated files are out of date." >&2
  echo "To fix, run: $*" >&2
fi
exit 1
