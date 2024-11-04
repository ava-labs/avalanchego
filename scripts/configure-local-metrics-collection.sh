#!/usr/bin/env bash

set -euo pipefail

# Configures prometheus and promtail to collect metrics and logs from
# a local node.
#
# To start metrics collection: ./scripts/run_prometheus.sh
# To start log collection: ./scripts/run_promtail.sh

API_PORT="${API_PORT:-9650}"

LOG_PATH="${LOG_PATH:-${HOME}/.avalanchego/logs}"

# Generate a uuid to uniquely identify the collected metrics
METRICS_UUID="$(uuidgen)"

mkdir -p "${HOME}"/.tmpnet/prometheus/file_sd_configs
cat >"${HOME}"/.tmpnet/prometheus/file_sd_configs/local.json <<EOL
[
  {
    "labels": {
      "network_uuid": "${METRICS_UUID}"
    },
    "targets": [
      "127.0.0.1:${API_PORT}"
    ]
  }
]
EOL

mkdir -p "${HOME}"/.tmpnet/promtail/file_sd_configs
cat >"${HOME}"/.tmpnet/promtail/file_sd_configs/local.json <<EOL
[
  {
    "labels": {
      "__path__": "${LOG_PATH}/*.log",
      "network_uuid": "${METRICS_UUID}"
    },
    "targets": [
      "localhost"
    ]
  }
]
EOL

echo "Prometheus and Loki have been configured to collect metrics and logs from the local node."
echo "Grafana link: https://grafana-poc.avax-dev.network/d/kBQpRdWnk/avalanche-main-dashboard?var-filter=network_uuid%7C%3D%7C${METRICS_UUID}"
