#!/usr/bin/env bash
set -euo pipefail

# Check Go's dry-run output. Link flags and VCS metadata can make Go recompile
# executable main packages, including the generated test main. Require cached
# compilation for their dependencies, but allow main compilation and linking.
if [[ $# -ne 1 ]]; then
  echo "usage: $0 <go-dry-run-log>" >&2
  exit 2
fi

if [[ ! -s "$1" ]]; then
  echo 'no Go dry-run output found' >&2
  exit 1
fi

if grep -E '(^|[[:space:]])[^[:space:]]*/(compile|asm|cgo)([[:space:]]|$)|[[:space:]]-c[[:space:]]' "$1" |
  grep -vE '/compile[[:space:]].*[[:space:]]-p[[:space:]]main([[:space:]]|$)'; then
  echo 'exact Go build-cache hit requires package compilation' >&2
  exit 1
fi
