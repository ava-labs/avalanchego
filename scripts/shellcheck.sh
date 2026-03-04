#!/usr/bin/env bash

set -euo pipefail

# This script can also be used to correct the problems detected by shellcheck by invoking as follows:
#
# ./scripts/tests.shellcheck.sh -f diff | git apply
#

if ! [[ "$0" =~ scripts/shellcheck.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# `find *` is the simplest way to ensure find does not include a
# leading `.` in filenames it emits. A leading `.` will prevent the
# use of `git apply` to fix reported shellcheck issues. This is
# compatible with both macos and linux (unlike the use of -printf).
# We exclude the graft/coreth and graft/subnet-evm directories to 
# avoid linting files that should be run from a different location 
# within the repo, as there are false positives.
#
# shellcheck disable=SC2035
find * \( -path 'graft/coreth' -o -path 'graft/subnet-evm' \) -prune -o -name '*.sh' -type f -print0 | xargs -0 shellcheck "${@}"
