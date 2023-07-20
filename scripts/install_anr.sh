#!/usr/bin/env bash
set -e
set -o nounset
set -o pipefail

# Avalanche root directory
AVALANCHE_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

#################################
# download avalanche-network-runner
# https://github.com/ava-labs/avalanche-network-runner
GOARCH=$(go env GOARCH)
GOOS=$(go env GOOS)
NETWORK_RUNNER_VERSION=1.3.9
anr_workdir=${ANR_WORKDIR:-"/tmp"}
DOWNLOAD_PATH=${anr_workdir}/avalanche-network-runner-v${NETWORK_RUNNER_VERSION}.tar.gz
DOWNLOAD_URL="https://github.com/ava-labs/avalanche-network-runner/releases/download/v${NETWORK_RUNNER_VERSION}/avalanche-network-runner_${NETWORK_RUNNER_VERSION}_${GOOS}_${GOARCH}.tar.gz"
echo "Installing avalanche-network-runner ${NETWORK_RUNNER_VERSION} to ${anr_workdir}/avalanche-network-runner"

# download only if not already downloaded
if [ ! -f "$DOWNLOAD_PATH" ]; then
  echo "downloading avalanche-network-runner ${NETWORK_RUNNER_VERSION} at ${DOWNLOAD_URL} to ${DOWNLOAD_PATH}"
  curl --fail -L ${DOWNLOAD_URL} -o ${DOWNLOAD_PATH}
else
  echo "avalanche-network-runner ${NETWORK_RUNNER_VERSION} already downloaded at ${DOWNLOAD_PATH}"
fi

rm -f ${anr_workdir}/avalanche-network-runner

echo "extracting downloaded avalanche-network-runner"
tar xzvf ${DOWNLOAD_PATH} -C ${anr_workdir}
${anr_workdir}/avalanche-network-runner -h
