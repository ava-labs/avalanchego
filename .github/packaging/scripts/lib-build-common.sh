#!/usr/bin/env bash

# Shared functions for RPM and DEB package build scripts.
#
# Sourced (not executed) by build-package.sh.

: "${REPO_ROOT:?REPO_ROOT must be set by the caller}"

readonly PACKAGER_NAME="Ava Labs"
readonly PACKAGER_EMAIL="security@avalabs.org"
readonly EPHEMERAL_GPG_PASSPHRASE="avalanchego-ephemeral-gpg-passphrase"

use_ephemeral_gpg_passphrase() {
    local passphrase_env="${1:?passphrase env var required}"

    declare -gx "${passphrase_env}=${EPHEMERAL_GPG_PASSPHRASE}"
}

# Initialize the build environment inside the container.
# Marks the bind-mounted source tree as git-safe, sources project
# scripts (constants.sh, git_commit.sh), and disables Go VCS stamping.
init_build_env() {
    if ! git -C "${REPO_ROOT}" rev-parse HEAD &>/dev/null; then
        git config --global --add safe.directory "${REPO_ROOT}"
    fi

    # shellcheck disable=SC1091
    source "${REPO_ROOT}/scripts/constants.sh"
    # shellcheck disable=SC1091
    source "${REPO_ROOT}/scripts/git_commit.sh"

    # shellcheck disable=SC2154  # git_commit is set by git_commit.sh sourced above
    echo "Git commit: ${git_commit}"

    # Disable Go's automatic VCS stamping — the commit hash is passed
    # explicitly via AVALANCHEGO_COMMIT and -ldflags instead.
    export GOFLAGS="${GOFLAGS:-} -buildvcs=false"
}

# Build the binary for the specified package.
# Sets BINARY_PATH (global) as a side effect.
#
# Args: package_name ("avalanchego" or "subnet-evm")
build_binary() {
    local package="${1:?package name required}"

    case "${package}" in
        avalanchego)
            echo "Building avalanchego..."
            "${REPO_ROOT}/scripts/build.sh"
            # avalanchego_path is set by scripts/constants.sh, sourced in init_build_env
            # shellcheck disable=SC2154
            BINARY_PATH="${avalanchego_path}"
            ;;
        subnet-evm)
            echo "Building subnet-evm..."
            resolve_subnet_evm_vm_id
            echo "Subnet-EVM VM ID: ${SUBNET_EVM_VM_ID}"

            BINARY_PATH="${REPO_ROOT}/build/subnet-evm"
            (cd "${REPO_ROOT}/graft/subnet-evm" && ./scripts/build.sh "${BINARY_PATH}")
            ;;
        *)
            echo "Unknown package: ${package}" >&2
            return 1
            ;;
    esac

    echo "Binary built at: ${BINARY_PATH}"
}

# Resolve the subnet-evm VM ID from the canonical constants file.
# Sets SUBNET_EVM_VM_ID (global) as a side effect.
resolve_subnet_evm_vm_id() {
    SUBNET_EVM_VM_ID="$(
        {
            # shellcheck disable=SC1091
            source "${REPO_ROOT}/graft/subnet-evm/scripts/constants.sh"
            # shellcheck disable=SC2154
            : "${DEFAULT_VM_ID:?DEFAULT_VM_ID must be set by constants.sh}"
        } >&2
        echo "${DEFAULT_VM_ID}"
    )"
    export SUBNET_EVM_VM_ID
}

# Generate the nfpm changelog file.
#
# Args: version (semver without "v" prefix)
generate_changelog() {
    local version="${1:?version required}"
    : "${NFPM_CHANGELOG:?NFPM_CHANGELOG must be set by the caller}"

    mkdir -p "$(dirname "${NFPM_CHANGELOG}")"
    cat > "${NFPM_CHANGELOG}" <<EOF
---
- semver: ${version}
  date: $(date -u +%Y-%m-%dT%H:%M:%SZ)
  packager: ${PACKAGER_NAME} <${PACKAGER_EMAIL}>
  changes:
    - note: "See https://github.com/ava-labs/avalanchego/releases/tag/v${version}"
EOF

    # Sanity-check the heredoc wrote the version line (catches silent
    # substitution failures: empty file, unresolved ${version}, etc.)
    if ! grep -q "^- semver: ${version}\$" "${NFPM_CHANGELOG}"; then
        echo "ERROR: generated changelog ${NFPM_CHANGELOG} missing 'semver: ${version}'" >&2
        return 1
    fi
}

# Set up GPG signing (import provided key, reuse ephemeral, or generate new).
#
# Args:
#   gpg_key_file   - path to GPG private key, or empty for ephemeral
#   public_key_out - output path for exported public key
#   key_label      - label for ephemeral key ("RPM" or "DEB")
setup_gpg() {
    local gpg_key_file="${1:-}"
    local public_key_out="${2:?public key output path required}"
    local key_label="${3:-Package}"
    : "${NFPM_SIGNING_KEY:?NFPM_SIGNING_KEY must be set by the caller}"

    mkdir -p "$(dirname "${NFPM_SIGNING_KEY}")"

    if [[ -n "${gpg_key_file}" ]]; then
        echo "Using provided GPG key for signing"
        gpg --batch --import "${gpg_key_file}"
        cp "${gpg_key_file}" "${NFPM_SIGNING_KEY}"
    elif [[ -f "${NFPM_SIGNING_KEY}" ]]; then
        # avalanchego and subnet-evm builds run in separate docker invocations
        # but share the on-disk key via the bind-mounted build/gpg directory so
        # the validator's single exported public key verifies both RPMs.
        echo "Reusing existing ephemeral GPG key"
        gpg --batch --import "${NFPM_SIGNING_KEY}"
    else
        echo "Generating ephemeral GPG key for signing"
        gpg --batch --pinentry-mode loopback --gen-key <<GPGEOF
Key-Type: RSA
Key-Length: 4096
Subkey-Type: RSA
Subkey-Length: 4096
Name-Real: AvalancheGo ${key_label} Signing (ephemeral)
Name-Email: ${PACKAGER_EMAIL}
Expire-Date: 1d
Passphrase: ${EPHEMERAL_GPG_PASSPHRASE}
%commit
GPGEOF
        gpg --batch --pinentry-mode loopback --passphrase "${EPHEMERAL_GPG_PASSPHRASE}" \
            --armor --export-secret-keys "${PACKAGER_EMAIL}" > "${NFPM_SIGNING_KEY}"
    fi

    gpg --batch --armor --export "${PACKAGER_EMAIL}" > "${public_key_out}"
    echo "GPG public key exported to: ${public_key_out}"
}

# Package with nfpm after envsubst preprocessing.
#
# nfpm does not expand env vars in top-level fields (changelog,
# signature.key_file). Preprocess the config template with envsubst
# so all ${VAR} references resolve before nfpm sees them.
run_nfpm_package() {
    # path to nfpm YAML template (with ${VAR} placeholders)
    local config_template="${1:?config template path required}"
    # output path for the preprocessed config
    local resolved_path="${2:?resolved config path required}"
    # nfpm packager name ("rpm" or "deb")
    local packager="${3:?packager name required}"
    # output path for the built package
    local target_path="${4:?target path required}"

    mkdir -p "$(dirname "${target_path}")"

    envsubst < "${config_template}" > "${resolved_path}"

    echo "Packaging $(basename "${target_path}")..."
    nfpm package \
        --config "${resolved_path}" \
        --packager "${packager}" \
        --target "${target_path}"
}

# Verify that all expected package files exist in a directory.
#
# Args: pkg_dir file1 [file2 ...]
assert_files_exist() {
    local pkg_dir="${1:?package directory required}"; shift
    local missing=()
    for f in "$@"; do
        [[ -f "${pkg_dir}/${f}" ]] || missing+=("${pkg_dir}/${f}")
    done
    if (( ${#missing[@]} > 0 )); then
        printf 'ERROR: expected file not found: %s\n' "${missing[@]}" >&2
        exit 1
    fi
}
