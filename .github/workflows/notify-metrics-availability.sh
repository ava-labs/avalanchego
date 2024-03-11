#!/usr/bin/env bash

set -euo pipefail

# Timestamps are in seconds
from_timestamp="$(date '+%s')"
monitoring_period=600 # 10 minutes
to_timestamp="$((${from_timestamp} + ${monitoring_period}))"

# Grafana expects microseconds, so pad timestamps with 3 zeros
metrics_url="${PROMETHEUS_URL}&var-filter=gh_job_id%7C%3D%7C${GH_JOB_ID}&from=${from_timestamp}000&to=${to_timestamp}000"

echo "::notice links::metrics ${metrics_url}"
