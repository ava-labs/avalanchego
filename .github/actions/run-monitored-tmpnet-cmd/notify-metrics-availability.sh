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

echo "Grafana: ${metrics_url}"
