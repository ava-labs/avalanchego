#!/usr/bin/env bash

set -euo pipefail

# - Starts a promtail instance to collect logs from nodes running locally and in CI.
#
# - promtail will remain running in the background and will forward logs to the
#   specified Loki endpoint.
#
# - Each node is configured with a file written to ~/.tmpnet/promtail/file_sd_configs/
#
# - To stop the running instance:
#     $ kill -9 `cat ~/.tmpnet/promtail/run.pid` && rm ~/.tmpnet/promtail/run.pid

# e.g.,
# LOKI_USERNAME=<username> LOKI_PASSWORD=<password> ./scripts/run_promtail.sh
if ! [[ "$0" =~ scripts/run_promtail.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

CMD=promtail

if ! command -v "${CMD}" &> /dev/null; then
  echo "promtail not found, have you run 'nix develop'?"
  echo "To install nix: https://github.com/DeterminateSystems/nix-installer?tab=readme-ov-file#install-nix"
  exit 1
fi

PROMTAIL_WORKING_DIR="${HOME}/.tmpnet/promtail"
PIDFILE="${PROMTAIL_WORKING_DIR}"/run.pid

# First check if promtail is already running. A single instance can
# collect logs from all local temporary networks.
if pgrep --pidfile="${PIDFILE}" &> /dev/null; then
  echo "promtail is already running"
  exit 0
fi

LOKI_URL="${LOKI_URL:-https://loki-poc.avax-dev.network}"
if [[ -z "${LOKI_URL}" ]]; then
  echo "Please provide a value for LOKI_URL"
  exit 1
fi

LOKI_USERNAME="${LOKI_USERNAME:-}"
if [[ -z "${LOKI_USERNAME}" ]]; then
  echo "Please provide a value for LOKI_USERNAME"
  exit 1
fi

LOKI_PASSWORD="${LOKI_PASSWORD:-}"
if [[ -z "${LOKI_PASSWORD}" ]]; then
  echo "Please provide a value for LOKI_PASSWORD"
  exit 1
fi

# Configure promtail
FILE_SD_PATH="${PROMTAIL_WORKING_DIR}/file_sd_configs"
mkdir -p "${FILE_SD_PATH}"

CONFIG_PATH="${PROMTAIL_WORKING_DIR}/promtail.yaml"
cat > "${CONFIG_PATH}" <<EOL
server:
  http_listen_port: 0
  grpc_listen_port: 0

positions:
  filename: "${PROMTAIL_WORKING_DIR}/positions.yaml"

client:
  url: "${LOKI_URL}/api/prom/push"
  basic_auth:
    username: "${LOKI_USERNAME}"
    password: "${LOKI_PASSWORD}"

scrape_configs:
  - job_name: "avalanchego"
    file_sd_configs:
      - files:
          - '${FILE_SD_PATH}/*.json'
EOL
echo "Wrote configuration to ${CONFIG_PATH}"

echo "Starting promtail..."
cd "${PROMTAIL_WORKING_DIR}"
nohup "${CMD}" -config.file=promtail.yaml > promtail.log 2>&1 &
echo $! > "${PIDFILE}"
echo "promtail started with pid $(cat "${PIDFILE}")"
# shellcheck disable=SC2016
echo 'To stop promtail: "kill -SIGTERM `cat ~/.tmpnet/promtail/run.pid` && rm ~/.tmpnet/promtail/run.pid"'
