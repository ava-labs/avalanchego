#!/usr/bin/env bash

set -euo pipefail

# Configures prometheus and promtail to collect metrics and logs from
# a local node.

API_PORT="${API_PORT:-9650}"

LOGS_PATH="${LOGS_PATH:-${HOME}/.avalanchego/logs}"

# Generate a uuid to uniquely identify the collected metrics
METRICS_UUID="$(uuidgen)"

echo "Configuring metrics and log collection for a local node with API port ${API_PORT} and logs path ${LOGS_PATH}"

PROMETHEUS_CONFIG_PATH="${HOME}/.tmpnet/prometheus/file_sd_configs"
PROMETHEUS_CONFIG_FILE="${PROMETHEUS_CONFIG_PATH}/local.json"
mkdir -p "${PROMETHEUS_CONFIG_PATH}"
cat > "${PROMETHEUS_CONFIG_FILE}" <<EOL
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
echo "Wrote prometheus configuration to ${PROMETHEUS_CONFIG_FILE}"

PROMTAIL_CONFIG_PATH="${HOME}/.tmpnet/promtail/file_sd_configs"
PROMTAIL_CONFIG_FILE="${PROMTAIL_CONFIG_PATH}/local.json"
mkdir -p "${PROMTAIL_CONFIG_PATH}"
cat > "${PROMTAIL_CONFIG_FILE}" <<EOL
[
  {
    "labels": {
      "__path__": "${LOGS_PATH}/*.log",
      "network_uuid": "${METRICS_UUID}"
    },
    "targets": [
      "localhost"
    ]
  }
]
EOL
echo "Wrote promtail configuration to ${PROMTAIL_CONFIG_FILE}"

echo "Metrics collection by prometheus can be started with ./bin/tmpnetctl start-metrics-collector"
echo "Log collection by promtail can be started with ./bin/tmpnetctl start-logs-collector"

GRAFANA_URI="${GRAFANA_URI:-https://grafana-poc.avax-dev.network/d/kBQpRdWnk/avalanche-main-dashboard}"
GRAFANA_LINK="${GRAFANA_URI}?var-filter=network_uuid%7C%3D%7C${METRICS_UUID}"
METRICS_PATH="${HOME}/.avalanchego/metrics.txt"
echo "${GRAFANA_LINK}" > "${METRICS_PATH}"
echo "Metrics and logs can be viewed at: ${GRAFANA_LINK}"
echo "Link also saved to ${METRICS_PATH}"
