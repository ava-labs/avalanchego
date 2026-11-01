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

# Bare paths avoid the leading `./` that prevents `git apply` from applying
# fixes from shellcheck. Include .github when it exists to check local action
# helpers. Graft directories can also call this script without a .github
# directory. This works on both macOS and Linux without find's -printf.
# Exclude graft/coreth and graft/subnet-evm because linting them from the
# repository root produces false positives.
#
# shellcheck disable=SC2035
paths=(*)
if [[ -d .github ]]; then
  paths+=(.github)
fi
find "${paths[@]}" \( -path 'graft/coreth' -o -path 'graft/subnet-evm' \) -prune -o -name '*.sh' -type f -print0 | xargs -0 shellcheck "${@}"
