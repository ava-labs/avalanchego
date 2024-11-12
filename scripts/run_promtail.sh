#!/usr/bin/env bash

set -euo pipefail

# Starts a promtail instance to collect logs from temporary networks
# running locally and in CI.
#
# The promtail instance will remain running in the background and will forward
# logs to the central instance for all tmpnet networks.
#
# To stop it:
#
#   $ kill -9 `cat ~/.tmpnet/promtail/run.pid` && rm ~/.tmpnet/promtail/run.pid
#

# e.g.,
# LOKI_ID=<id> LOKI_PASSWORD=<password> ./scripts/run_promtail.sh
if ! [[ "$0" =~ scripts/run_promtail.sh ]]; then
  echo "must be run from repository root"
  exit 255
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

LOKI_ID="${LOKI_ID:-}"
if [[ -z "${LOKI_ID}" ]]; then
  echo "Please provide a value for LOKI_ID"
  exit 1
fi

LOKI_PASSWORD="${LOKI_PASSWORD:-}"
if [[ -z "${LOKI_PASSWORD}" ]]; then
  echo "Plase provide a value for LOKI_PASSWORD"
  exit 1
fi

# Version as of this writing
VERSION="v2.9.5"

# Ensure the promtail command is locally available
CMD=promtail
if ! command -v "${CMD}" &> /dev/null; then
  # Try to use a local version
  CMD="${PWD}/bin/promtail"
  if ! command -v "${CMD}" &> /dev/null; then
    echo "promtail not found, attempting to install..."
    # Determine the arch
    if which sw_vers &> /dev/null; then
      DIST="darwin-$(uname -m)"
    else
      ARCH="$(uname -i)"
      if [[ "${ARCH}" == "aarch64" ]]; then
        ARCH="arm64"
      elif [[ "${ARCH}" == "x86_64" ]]; then
        ARCH="amd64"
      fi
      DIST="linux-${ARCH}"
    fi

    # Install the specified release
    PROMTAIL_FILE="promtail-${DIST}"
    ZIP_PATH="/tmp/${PROMTAIL_FILE}.zip"
    BIN_DIR="$(dirname "${CMD}")"
    URL="https://github.com/grafana/loki/releases/download/${VERSION}/promtail-${DIST}.zip"
    curl -L -o "${ZIP_PATH}" "${URL}"
    unzip "${ZIP_PATH}" -d "${BIN_DIR}"
    mv "${BIN_DIR}/${PROMTAIL_FILE}" "${CMD}"
  fi
fi

# Configure promtail
FILE_SD_PATH="${PROMTAIL_WORKING_DIR}/file_sd_configs"
mkdir -p "${FILE_SD_PATH}"

echo "writing configuration..."
cat >"${PROMTAIL_WORKING_DIR}"/promtail.yaml <<EOL
server:
  http_listen_port: 0
  grpc_listen_port: 0

positions:
  filename: "${PROMTAIL_WORKING_DIR}/positions.yaml"

client:
  url: "${LOKI_URL}/api/prom/push"
  basic_auth:
    username: "${LOKI_ID}"
    password: "${LOKI_PASSWORD}"

scrape_configs:
  - job_name: "avalanchego"
    file_sd_configs:
      - files:
          - '${FILE_SD_PATH}/*.json'
EOL

echo "starting promtail..."
cd "${PROMTAIL_WORKING_DIR}"
nohup "${CMD}" -config.file=promtail.yaml > promtail.log 2>&1 &
echo $! > "${PIDFILE}"
echo "running with pid $(cat "${PIDFILE}")"
