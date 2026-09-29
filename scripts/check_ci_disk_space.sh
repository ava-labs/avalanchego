#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF' >&2
Usage: ./scripts/check_ci_disk_space.sh [--min-gb <integer>]
EOF
}

min_gb=20

while [[ $# -gt 0 ]]; do
  case "$1" in
    --min-gb)
      if [[ $# -lt 2 ]]; then
        usage
        exit 2
      fi
      min_gb="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

if ! [[ "${min_gb}" =~ ^[0-9]+$ ]]; then
  echo "--min-gb must be a non-negative integer" >&2
  exit 2
fi

if [[ -n "${CI_FORCE_FREE_GB:-}" ]]; then
  if ! [[ "${CI_FORCE_FREE_GB}" =~ ^[0-9]+$ ]]; then
    echo "CI_FORCE_FREE_GB must be a non-negative integer when set" >&2
    exit 2
  fi
  free_gb="${CI_FORCE_FREE_GB}"
else
  available_kb="$({ df -Pk / | awk 'NR==2 { print $4 }'; } | tr -d '[:space:]')"
  if ! [[ "${available_kb}" =~ ^[0-9]+$ ]]; then
    echo "failed to determine free disk on /" >&2
    exit 1
  fi
  free_gb="$((available_kb / 1024 / 1024))"
fi

below_threshold=false
status=healthy
if (( free_gb < min_gb )); then
  below_threshold=true
  status='below threshold'
fi

echo "Free disk on /: ${free_gb}G available (threshold: ${min_gb}G, status: ${status})"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  echo "below_threshold=${below_threshold}" >>"${GITHUB_OUTPUT}"
fi
