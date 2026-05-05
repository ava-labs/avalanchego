#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/build_subnet_evm_sae.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

source ./scripts/constants.sh
source ./scripts/git_commit.sh

BINARY_PATH="${AVALANCHEGO_BUILD_PATH:-${AVALANCHE_PATH}/build}/subnet-evm-sae"

echo "Building Subnet-EVM SAE plugin @ GitCommit: ${git_commit} at ${BINARY_PATH}"
# shellcheck disable=SC2086
go build -ldflags "-X github.com/ava-labs/avalanchego/version.GitCommit=${git_commit} ${static_ld_flags}" \
  -o "${BINARY_PATH}" ./vms/subnetevm/plugin/

# Symlink to both global and local plugin directories under the SubnetEVMID
# (`srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy`) so it replaces the
# legacy `graft/subnet-evm` plugin at runtime. The two are mutually exclusive:
# whichever was symlinked last is used by avalanchego when launching subnets
# whose chain VM ID is `constants.SubnetEVMID`.
LOCAL_PLUGIN_PATH="${PWD}/build/plugins"
GLOBAL_PLUGIN_PATH="${HOME}/.avalanchego/plugins"
SUBNET_EVM_VM_ID="srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"
for plugin_dir in "${GLOBAL_PLUGIN_PATH}" "${LOCAL_PLUGIN_PATH}"; do
  PLUGIN_PATH="${plugin_dir}/${SUBNET_EVM_VM_ID}"
  echo "Symlinking ${BINARY_PATH} to ${PLUGIN_PATH}"
  mkdir -p "${plugin_dir}"
  ln -sf "${BINARY_PATH}" "${PLUGIN_PATH}"
done
