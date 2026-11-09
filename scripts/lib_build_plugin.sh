#!/usr/bin/env bash
#
# Shared helper for building VM plugins.
#
# Provides:
#   PLUGIN_BUILD_DIR - Default directory for plugin binaries
#   build_plugin BINARY_PATH VM_ID PACKAGE
#     Builds PACKAGE (relative to the working directory) to BINARY_PATH and
#     symlinks it as VM_ID into the local and global plugin directories.

# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")"/constants.sh
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")"/git_commit.sh

# shellcheck disable=SC2034 # used by the scripts that source this one
PLUGIN_BUILD_DIR="${AVALANCHEGO_BUILD_PATH:-${AVALANCHE_PATH}/build}"

# shellcheck disable=SC2154 # git_commit and go_ldflags are set by git_commit.sh
function build_plugin {
  local binary_path=$1
  local vm_id=$2
  local package=$3

  echo "Building ${package} @ GitCommit: ${git_commit} at ${binary_path}"
  go build -ldflags "${go_ldflags}" \
    -o "${binary_path}" "${package}"

  # Symlink to both global and local plugin directories to simplify
  # usage for testing. The local directory should be preferred but the
  # global directory remains supported for backwards compatibility.
  local abs_binary_path
  abs_binary_path="$(cd "$(dirname "${binary_path}")" && pwd)/$(basename "${binary_path}")"
  local local_plugin_dir="${AVALANCHE_PATH}/build/plugins"
  local global_plugin_dir="${HOME}/.avalanchego/plugins"
  for plugin_dir in "${global_plugin_dir}" "${local_plugin_dir}"; do
    local plugin_path="${plugin_dir}/${vm_id}"
    echo "Symlinking ${abs_binary_path} to ${plugin_path}"
    mkdir -p "${plugin_dir}"
    ln -sf "${abs_binary_path}" "${plugin_path}"
  done
}
