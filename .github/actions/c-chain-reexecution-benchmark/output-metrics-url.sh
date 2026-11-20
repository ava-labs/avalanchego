#!/usr/bin/env bash

# WARNING: This file is a duplication of:
#   - .github/actions/run-monitored-tmpnet-cmd/output-metrics-url.sh (source of truth)
# Changes must be made to BOTH files.

set -euo pipefail

# Timestamps are in seconds. METRICS_START_TIMESTAMP should be set to the time
# the monitored work started. If METRICS_END_TIMESTAMP is not set, the link
# displays metrics up to the time it is viewed so that it can be shared while
# the monitored work is still running.
from_timestamp="${METRICS_START_TIMESTAMP:-$(date '+%s')}"
if [[ -n "${METRICS_END_TIMESTAMP:-}" ]]; then
  # Pad the end of the range to include metrics scraped just before the end.
  end_padding=60
  to="$((METRICS_END_TIMESTAMP + end_padding))000"
else
  to="now"
fi

# Grafana expects milliseconds, so pad timestamps with 3 zeros
metrics_url="${GRAFANA_URL}&var-filter=gh_job_id%7C%3D%7C${GH_JOB_ID}&from=${from_timestamp}000&to=${to}"

# Optionally ensure that the link displays metrics only for the shared
# network rather than mixing it with the results for private networks.
if [[ -n "${FILTER_BY_OWNER:-}" ]]; then
  metrics_url="${metrics_url}&var-filter=network_owner%7C%3D%7C${FILTER_BY_OWNER}"
fi

echo "${metrics_url}"
