#!/usr/bin/env bash

# Post-build validation of linux tarballs.
#
# Verifies detached signatures and runs smoke tests in a fresh Ubuntu
# container, mirroring how a downstream consumer would verify and use
# the released artifacts.
#
# Required env vars:
#   TAG            - Git tag (e.g., "v1.14.1")
#   GIT_COMMIT     - Full git commit hash used to build the binaries
#
# Optional env vars:
#   TGZ_ARCH       - Tarball architecture ("amd64" or "arm64"), defaults to host

set -euo pipefail

: "${TAG:?TAG must be set}"
: "${GIT_COMMIT:?GIT_COMMIT must be set}"

if [[ -z "${TGZ_ARCH:-}" ]]; then
    arch=$(uname -m)
    case "${arch}" in
        x86_64)        TGZ_ARCH="amd64" ;;
        arm64|aarch64) TGZ_ARCH="arm64" ;;
        *)             TGZ_ARCH="${arch}" ;;
    esac
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TGZ_DIR="${REPO_ROOT}/build/tgz"

# Verify expected files exist
for f in \
    "avalanchego-linux-${TGZ_ARCH}-${TAG}.tar.gz" \
    "subnet-evm-linux-${TGZ_ARCH}-${TAG}.tar.gz" \
; do
    if [[ ! -f "${TGZ_DIR}/${f}" ]]; then
        echo "ERROR: expected file not found: ${TGZ_DIR}/${f}" >&2
        exit 1
    fi
done

echo "=== Validating tarballs in fresh Ubuntu 22.04 container ==="
docker run --rm \
    -v "${TGZ_DIR}:/tgz:ro" \
    -e TAG \
    -e TGZ_ARCH \
    -e GIT_COMMIT \
    ubuntu:22.04 \
    bash -euxc '
        export DEBIAN_FRONTEND=noninteractive
        apt-get update
        apt-get install -y --no-install-recommends gnupg2 ca-certificates

        # Verify signatures if public key available
        if [[ -f /tgz/GPG-KEY-avalanchego ]]; then
            gpg --batch --import /tgz/GPG-KEY-avalanchego
            gpg --batch --verify "/tgz/avalanchego-linux-${TGZ_ARCH}-${TAG}.tar.gz.sig" \
                                  "/tgz/avalanchego-linux-${TGZ_ARCH}-${TAG}.tar.gz"
            gpg --batch --verify "/tgz/subnet-evm-linux-${TGZ_ARCH}-${TAG}.tar.gz.sig" \
                                  "/tgz/subnet-evm-linux-${TGZ_ARCH}-${TAG}.tar.gz"
        else
            echo "Skipping GPG verification (unsigned build)"
        fi

        # Extract both tarballs
        mkdir -p /work
        cd /work
        tar -xzf "/tgz/avalanchego-linux-${TGZ_ARCH}-${TAG}.tar.gz"
        tar -xzf "/tgz/subnet-evm-linux-${TGZ_ARCH}-${TAG}.tar.gz"

        # Smoke test avalanchego
        AVA_BIN="/work/avalanchego-${TAG}/avalanchego"
        if [[ ! -x "${AVA_BIN}" ]]; then
            echo "ERROR: avalanchego binary not found or not executable" >&2
            exit 1
        fi
        ava_output=$("${AVA_BIN}" --version)
        echo "avalanchego --version: ${ava_output}"
        if [[ "${ava_output}" != avalanchego/* ]]; then
            echo "ERROR: --version output does not start with avalanchego/" >&2
            exit 1
        fi
        if [[ "${ava_output}" != *"${GIT_COMMIT}"* ]]; then
            echo "ERROR: avalanchego --version output does not contain expected commit ${GIT_COMMIT}" >&2
            exit 1
        fi

        # Smoke test subnet-evm
        EVM_BIN="/work/subnet-evm-${TAG}/subnet-evm"
        if [[ ! -x "${EVM_BIN}" ]]; then
            echo "ERROR: subnet-evm binary not found or not executable" >&2
            exit 1
        fi
        evm_output=$("${EVM_BIN}" --version)
        echo "subnet-evm --version: ${evm_output}"
        if [[ "${evm_output}" != *"${GIT_COMMIT}"* ]]; then
            echo "ERROR: subnet-evm --version output does not contain expected commit ${GIT_COMMIT}" >&2
            exit 1
        fi

        echo "All tarball validations passed"
    '

echo "=== Tarball validation complete ==="
