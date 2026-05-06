#!/usr/bin/env bash

# Smoke test installed avalanchego and subnet-evm packages.
#
# Runs inside a validation container after packages have been installed.
# Verifies that binaries are present, executable, and report the expected
# version/commit information.
#
# Args:
#   $1 - path to avalanchego binary
#   $2 - plugin directory path (subnet-evm VM ID appended by this script)
#   $3 - expected full git commit hash
#   $4 - subnet-evm VM ID

set -euxo pipefail

AVAGO_BIN="${1:?avalanchego binary path required}"
PLUGIN_DIR="${2:?plugin directory path required}"
GIT_COMMIT="${3:?git commit hash required}"
SUBNET_EVM_VM_ID="${4:?subnet-evm VM ID required}"

# ── Smoke test avalanchego ────────────────────────────────────────

output=$("${AVAGO_BIN}" --version)
echo "avalanchego --version: ${output}"
if [[ "${output}" != avalanchego/* ]]; then
    echo "ERROR: --version output does not start with avalanchego/" >&2
    exit 1
fi
if [[ "${output}" != *"${GIT_COMMIT}"* ]]; then
    echo "ERROR: avalanchego --version output does not contain expected commit ${GIT_COMMIT}" >&2
    echo "Output: ${output}" >&2
    exit 1
fi

# ── Verify subnet-evm plugin ─────────────────────────────────────

plugin="${PLUGIN_DIR}/${SUBNET_EVM_VM_ID}"
if [[ ! -x "${plugin}" ]]; then
    echo "ERROR: subnet-evm plugin not found or not executable at ${plugin}" >&2
    exit 1
fi

evm_output=$("${plugin}" --version)
echo "subnet-evm --version: ${evm_output}"
if [[ "${evm_output}" != *"${GIT_COMMIT}"* ]]; then
    echo "ERROR: subnet-evm --version output does not contain expected commit ${GIT_COMMIT}" >&2
    echo "Output: ${evm_output}" >&2
    exit 1
fi

echo "All package validations passed"
