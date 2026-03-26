#!/usr/bin/env bash

set -euo pipefail

: "${DEB_PATH:?DEB_PATH must be set}"
: "${PUBLIC_KEY_PATH:?PUBLIC_KEY_PATH must be set}"
: "${GIT_COMMIT:?GIT_COMMIT must be set}"
: "${RELEASE:?RELEASE must be set}"

if [[ ! -f "${DEB_PATH}" ]]; then
    echo "Missing DEB package: ${DEB_PATH}" >&2
    exit 1
fi

if [[ ! -f "${PUBLIC_KEY_PATH}" ]]; then
    echo "Missing package signing public key: ${PUBLIC_KEY_PATH}" >&2
    exit 1
fi

case "${RELEASE}" in
    jammy) UBUNTU_IMAGE="ubuntu:22.04" ;;
    noble) UBUNTU_IMAGE="ubuntu:24.04" ;;
    *)
        echo "Unsupported Ubuntu release: ${RELEASE}" >&2
        exit 1
        ;;
esac

DEB_DIR="$(cd "$(dirname "${DEB_PATH}")" && pwd)"
DEB_NAME="$(basename "${DEB_PATH}")"
PUBLIC_KEY_NAME="$(basename "${PUBLIC_KEY_PATH}")"

echo "=== Validating ${DEB_NAME} in fresh ${UBUNTU_IMAGE} container ==="
docker run --rm \
    -v "${DEB_DIR}:/input:ro" \
    "${UBUNTU_IMAGE}" \
    bash -euxo pipefail -c '
        export DEBIAN_FRONTEND=noninteractive
        apt-get update
        apt-get install -y ca-certificates dpkg-sig gnupg

        gpg --batch --import "/input/'"${PUBLIC_KEY_NAME}"'"
        dpkg-sig --verify "/input/'"${DEB_NAME}"'"
        apt-get install -y "/input/'"${DEB_NAME}"'"

        output=$(/usr/local/bin/avalanchego --version)
        echo "avalanchego --version: ${output}"
        if [[ "${output}" != avalanchego/* ]]; then
            echo "ERROR: --version output does not start with avalanchego/" >&2
            exit 1
        fi
        if [[ "${output}" != *"'"${GIT_COMMIT}"'"* ]]; then
            echo "ERROR: --version output does not contain expected commit '"${GIT_COMMIT}"'" >&2
            exit 1
        fi
    '

echo "=== DEB validation complete ==="
