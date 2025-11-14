#!/usr/bin/env bash

# WARNING: This file is a duplication of:
#   - .github/actions/run-monitored-tmpnet-cmd/output-metrics-url.sh (source of truth)
# Changes must be made to BOTH files.

set -euo pipefail

# Timestamps are in seconds
from_timestamp="$(date '+%s')"
monitoring_period=900 # 15 minutes
to_timestamp="$((from_timestamp + monitoring_period))"

# Export timestamps to GitHub environment
if [ -n "${GITHUB_ENV:-}" ]; then
  echo "METRICS_FROM_TIMESTAMP=${from_timestamp}" >> "$GITHUB_ENV"
  echo "METRICS_TO_TIMESTAMP=${to_timestamp}" >> "$GITHUB_ENV"
fi

# Grafana expects microseconds, so pad timestamps with 3 zeros
metrics_url="${GRAFANA_URL}&var-filter=gh_job_id%7C%3D%7C${GH_JOB_ID}&from=${from_timestamp}000&to=${to_timestamp}000"

# Optionally ensure that the link displays metrics only for the shared
# network rather than mixing it with the results for private networks.
if [[ -n "${FILTER_BY_OWNER:-}" ]]; then
  metrics_url="${metrics_url}&var-filter=network_owner%7C%3D%7C${FILTER_BY_OWNER}"
fi

echo "${metrics_url}"
