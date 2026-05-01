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
# Target architecture is derived from `uname -m`. The validation
# container is launched with --platform pinned to host arch, matching
# the arch the tarballs were built for, so this always lines up.

set -euo pipefail

: "${TAG:?TAG must be set}"
: "${GIT_COMMIT:?GIT_COMMIT must be set}"

# Map uname -m to deb-style arch (aarch64 -> arm64). The script owns its
# own arch determination — we don't accept TGZ_ARCH from env, since
# Task v3 forwards parent shell env vars in a way that would let a
# caller-supplied TGZ_ARCH=<other-arch> mislabel the validation lookup.
arch=$(uname -m)
case "${arch}" in
    x86_64)        TGZ_ARCH="amd64" ;;
    arm64|aarch64) TGZ_ARCH="arm64" ;;
    *) echo "Unsupported arch: ${arch}" >&2; exit 1 ;;
esac
# Export so the validation container (launched below) sees it via `-e TGZ_ARCH`.
export TGZ_ARCH

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
# Pin --platform to host arch (TGZ_ARCH is always host arch here) so
# DOCKER_DEFAULT_PLATFORM doesn't cause Docker to try a non-host
# manifest of ubuntu:22.04 and fail to launch.
docker run --rm \
    --platform "linux/${TGZ_ARCH}" \
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
