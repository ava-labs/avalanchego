#!/usr/bin/env bash

set -euo pipefail

# Timestamps are in seconds
from_timestamp="$(date '+%s')"
monitoring_period=900 # 15 minutes
to_timestamp="$((from_timestamp + monitoring_period))"

# Grafana expects microseconds, so pad timestamps with 3 zeros
metrics_url="${GRAFANA_URL}&var-filter=gh_job_id%7C%3D%7C${GH_JOB_ID}&from=${from_timestamp}000&to=${to_timestamp}000"

# Optionally ensure that the link displays metrics only for the shared
# network rather than mixing it with the results for private networks.
if [[ -n "${FILTER_BY_OWNER:-}" ]]; then
  metrics_url="${metrics_url}&var-filter=network_owner%7C%3D%7C${FILTER_BY_OWNER}"
fi

echo "grafana link for shared network logs and metrics: ${metrics_url}"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  echo "metrics_url=${metrics_url}" >> $GITHUB_OUTPUT

  # Save metadata for analysis
  mkdir -p /tmp/run-metadata
  cat > /tmp/run-metadata/run_metadata.json << EOF
{
  "run_id": "${GH_RUN_ID:-}",
  "run_attempt": "${GH_RUN_ATTEMPT:-}",
  "job_id": "${GH_JOB_ID}",
  "metrics_url": "${metrics_url}",
  "start_timestamp": ${from_timestamp},
  "end_timestamp": ${to_timestamp},
  "repository": "${GH_REPO:-}"
}
EOF
fi
