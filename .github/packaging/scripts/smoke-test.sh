#!/usr/bin/env bash

# Smoke test installed avalanchego and subnet-evm packages.
#
# Runs inside a validation container after packages have been installed.
# Verifies that binaries are present, executable, and report the expected
# version/commit information.

set -euxo pipefail

AVAGO_BIN="${1:?avalanchego binary path required}"
SUBNET_EVM_BIN="${2:?subnet-evm plugin binary path required}"
GIT_COMMIT="${3:?expected full git commit hash required}"

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

if [[ ! -x "${SUBNET_EVM_BIN}" ]]; then
    echo "ERROR: subnet-evm plugin not found or not executable at ${SUBNET_EVM_BIN}" >&2
    exit 1
fi

evm_output=$("${SUBNET_EVM_BIN}" --version)
echo "subnet-evm --version: ${evm_output}"
if [[ "${evm_output}" != *"${GIT_COMMIT}"* ]]; then
    echo "ERROR: subnet-evm --version output does not contain expected commit ${GIT_COMMIT}" >&2
    echo "Output: ${evm_output}" >&2
    exit 1
fi

echo "All package validations passed"
