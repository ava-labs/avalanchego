#!/usr/bin/env bash

set -euo pipefail

if [[ -z "${NEEDS_JSON:-}" ]]; then
  echo "NEEDS_JSON must be set" >&2
  exit 2
fi

if command -v jq >/dev/null 2>&1; then
  jq_cmd=(jq)
else
  jq_cmd=(nix run nixpkgs#jq --)
fi

failed_jobs="$(
  printf '%s\n' "$NEEDS_JSON" | "${jq_cmd[@]}" -r '
    to_entries
    | map(select(.value.result != "success"))
    | .[]
    | "\(.key): \(.value.result)"
  '
)"

if [[ -n "$failed_jobs" ]]; then
  echo "Required jobs failed:" >&2
  printf '%s\n' "$failed_jobs" >&2
  echo >&2
  echo "Full needs JSON:" >&2
  printf '%s\n' "$NEEDS_JSON" | "${jq_cmd[@]}" . >&2
  exit 1
fi
