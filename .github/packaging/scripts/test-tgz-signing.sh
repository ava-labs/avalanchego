#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
TGZ_DIR="${REPO_ROOT}/build/tgz"

test_root=$(mktemp -d)
cleanup() {
    rm -rf "${test_root}"
}
trap cleanup EXIT

stub_bin="${test_root}/bin"
mkdir -p "${stub_bin}" "${TGZ_DIR}"

host_tgz_arch() {
    local arch
    arch=$(uname -m)
    case "${arch}" in
        x86_64)        echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *) echo "Unsupported arch: ${arch}" >&2; exit 1 ;;
    esac
}

cat > "${stub_bin}/aws" <<'AWS'
#!/usr/bin/env bash
printf 'aws %s\n' "$*" >> "${AWS_LOG:?AWS_LOG must be set}"
AWS
chmod +x "${stub_bin}/aws"

cat > "${stub_bin}/docker" <<'DOCKER'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >> "${DOCKER_LOG:?DOCKER_LOG must be set}"
DOCKER
chmod +x "${stub_bin}/docker"

cat > "${stub_bin}/gpg" <<'GPG'
#!/usr/bin/env bash
printf 'gpg %s\n' "$*" >> "${GPG_LOG:?GPG_LOG must be set}"
for arg in "$@"; do
    if [[ "${arg}" == "--verify" ]]; then
        exit "${GPG_VERIFY_EXIT:-0}"
    fi
done
GPG
chmod +x "${stub_bin}/gpg"

tracked_files=()
track_file() {
    local f="$1"
    if [[ -e "${f}" ]]; then
        echo "Refusing to overwrite existing test fixture: ${f}" >&2
        exit 1
    fi
    tracked_files+=("${f}")
}

cleanup_fixtures() {
    local f
    for f in "${tracked_files[@]}"; do
        rm -f "${f}"
    done
    tracked_files=()
}
trap 'cleanup_fixtures; cleanup' EXIT

create_tgz_fixture() {
    local tag="$1"
    local arch="$2"
    local include_signatures="${3:-false}"
    local include_public_key="${4:-false}"
    local f

    for f in \
        "${TGZ_DIR}/avalanchego-linux-${arch}-${tag}.tar.gz" \
        "${TGZ_DIR}/subnet-evm-linux-${arch}-${tag}.tar.gz"
    do
        track_file "${f}"
        touch "${f}"
    done

    if [[ "${include_signatures}" == "true" ]]; then
        for f in \
            "${TGZ_DIR}/avalanchego-linux-${arch}-${tag}.tar.gz.sig" \
            "${TGZ_DIR}/subnet-evm-linux-${arch}-${tag}.tar.gz.sig"
        do
            track_file "${f}"
            touch "${f}"
        done
    fi

    if [[ "${include_public_key}" == "true" ]]; then
        f="${TGZ_DIR}/GPG-KEY-avalanchego"
        track_file "${f}"
        touch "${f}"
    fi
}

expect_failure_containing() {
    local expected="$1"
    shift
    local out="${test_root}/command.out"

    if "$@" > "${out}" 2>&1; then
        echo "Expected command to fail: $*" >&2
        cat "${out}" >&2
        exit 1
    fi

    if ! grep -q "${expected}" "${out}"; then
        echo "Expected failure output to contain: ${expected}" >&2
        cat "${out}" >&2
        exit 1
    fi
}

test_upload_requires_signatures() {
    local tag="v0.0.0-codex-upload-missing-sig-$$"
    create_tgz_fixture "${tag}" "amd64" false false

    expect_failure_containing "expected file not found" env \
        PATH="${stub_bin}:${PATH}" \
        AWS_LOG="${test_root}/aws.log" \
        BUCKET="example-bucket" \
        TGZ_ARCH="amd64" \
        TGZ_RELEASE="jammy" \
        TAG="${tag}" \
        "${SCRIPT_DIR}/upload-tgz.sh"
}

test_upload_verifies_signatures_before_uploading() {
    local tag="v0.0.0-codex-upload-bad-sig-$$"
    create_tgz_fixture "${tag}" "amd64" true true

    expect_failure_containing "signature verification failed" env \
        PATH="${stub_bin}:${PATH}" \
        AWS_LOG="${test_root}/aws.log" \
        GPG_LOG="${test_root}/gpg.log" \
        GPG_VERIFY_EXIT=1 \
        BUCKET="example-bucket" \
        TGZ_ARCH="amd64" \
        TGZ_RELEASE="jammy" \
        TAG="${tag}" \
        "${SCRIPT_DIR}/upload-tgz.sh"
}

test_validate_requires_signatures_and_public_key() {
    local tag="v0.0.0-codex-validate-missing-sig-$$"
    local arch
    arch=$(host_tgz_arch)
    create_tgz_fixture "${tag}" "${arch}" false false

    expect_failure_containing "expected file not found" env \
        PATH="${stub_bin}:${PATH}" \
        DOCKER_LOG="${test_root}/docker.log" \
        TAG="${tag}" \
        GIT_COMMIT="deadbeef" \
        "${SCRIPT_DIR}/validate-tgz.sh"
}

test_validate_requires_public_key() {
    local tag="v0.0.0-codex-validate-missing-key-$$"
    local arch
    arch=$(host_tgz_arch)
    create_tgz_fixture "${tag}" "${arch}" true false

    expect_failure_containing "GPG-KEY-avalanchego" env \
        PATH="${stub_bin}:${PATH}" \
        DOCKER_LOG="${test_root}/docker.log" \
        TAG="${tag}" \
        GIT_COMMIT="deadbeef" \
        "${SCRIPT_DIR}/validate-tgz.sh"
}

test_build_requires_passphrase_when_signing() {
    local key_file="${test_root}/private.key"
    local output_dir="${test_root}/out"
    printf 'private key placeholder\n' > "${key_file}"

    expect_failure_containing "GPG_PASSPHRASE must be set" env \
        PACKAGING_TAG="v0.0.0-codex-build-missing-passphrase" \
        OUTPUT_DIR="${output_dir}" \
        GPG_KEY_FILE="${key_file}" \
        AVALANCHEGO_COMMIT="deadbeef" \
        "${SCRIPT_DIR}/build-tgz.sh"
}

run_test() {
    "$1"
    cleanup_fixtures
}

run_test test_upload_requires_signatures
run_test test_upload_verifies_signatures_before_uploading
run_test test_validate_requires_signatures_and_public_key
run_test test_validate_requires_public_key
run_test test_build_requires_passphrase_when_signing

echo "tgz signing tests passed"
