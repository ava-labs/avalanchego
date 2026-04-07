#!/usr/bin/env bash

# Shared functions for RPM and DEB package build scripts.
#
# Sourced (not executed) by build-rpm.sh and build-deb.sh.
# All functions expect REPO_ROOT to be set by the caller.

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

    # shellcheck disable=SC2154
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
            # shellcheck disable=SC2154
            BINARY_PATH="${avalanchego_path}"
            ;;
        subnet-evm)
            echo "Building subnet-evm..."
            resolve_subnet_evm_vm_id
            echo "Subnet-EVM VM ID: ${SUBNET_EVM_VM_ID}"

            SUBNET_EVM_BINARY="${REPO_ROOT}/build/subnet-evm"
            (cd "${REPO_ROOT}/graft/subnet-evm" && ./scripts/build.sh "${SUBNET_EVM_BINARY}")
            BINARY_PATH="${SUBNET_EVM_BINARY}"
            ;;
        *)
            echo "Unknown package: ${package}" >&2
            exit 1
            ;;
    esac

    echo "Binary built at: ${BINARY_PATH}"
}

# Resolve the subnet-evm VM ID from the canonical constants file.
# Sets SUBNET_EVM_VM_ID (global) as a side effect.
resolve_subnet_evm_vm_id() {
    SUBNET_EVM_VM_ID=$(
        grep '^DEFAULT_VM_ID=' "${REPO_ROOT}/graft/subnet-evm/scripts/constants.sh" \
        | cut -d'"' -f2
    )
    if [[ -z "${SUBNET_EVM_VM_ID}" ]]; then
        echo "ERROR: could not resolve SUBNET_EVM_VM_ID" >&2
        exit 1
    fi
    export SUBNET_EVM_VM_ID
}

# Generate the nfpm changelog file.
#
# Args: version (semver without "v" prefix)
generate_changelog() {
    local version="${1:?version required}"

    mkdir -p "$(dirname "${NFPM_CHANGELOG}")"
    cat > "${NFPM_CHANGELOG}" <<EOF
---
- semver: ${version}
  date: $(date -u +%Y-%m-%dT%H:%M:%SZ)
  packager: Ava Labs <security@avalabs.org>
  changes:
    - note: "See https://github.com/ava-labs/avalanchego/releases/tag/v${version}"
EOF
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

    mkdir -p "$(dirname "${NFPM_SIGNING_KEY}")"

    if [[ -n "${gpg_key_file}" ]]; then
        echo "Using provided GPG key for signing"
        gpg --batch --import "${gpg_key_file}"
        cp "${gpg_key_file}" "${NFPM_SIGNING_KEY}"
    elif [[ -f "${NFPM_SIGNING_KEY}" ]]; then
        echo "Reusing existing ephemeral GPG key"
        gpg --batch --import "${NFPM_SIGNING_KEY}"
    else
        echo "Generating ephemeral GPG key for signing"
        gpg --batch --gen-key <<GPGEOF
%no-protection
Key-Type: RSA
Key-Length: 4096
Subkey-Type: RSA
Subkey-Length: 4096
Name-Real: AvalancheGo ${key_label} Signing (ephemeral)
Name-Email: security@avalabs.org
Expire-Date: 1d
%commit
GPGEOF
        gpg --batch --armor --export-secret-keys "security@avalabs.org" > "${NFPM_SIGNING_KEY}"
    fi

    gpg --batch --armor --export "security@avalabs.org" > "${public_key_out}"
    echo "GPG public key exported to: ${public_key_out}"
}

# Package with nfpm after envsubst preprocessing.
#
# nfpm does not expand env vars in top-level fields (changelog,
# signature.key_file). Preprocess the config template with envsubst
# so all ${VAR} references resolve before nfpm sees them.
#
# Args:
#   config_template - path to nfpm YAML template (with ${VAR} placeholders)
#   resolved_path   - output path for the preprocessed config
#   packager        - nfpm packager name ("rpm" or "deb")
#   target_path     - output path for the built package
run_nfpm_package() {
    local config_template="${1:?config template path required}"
    local resolved_path="${2:?resolved config path required}"
    local packager="${3:?packager name required}"
    local target_path="${4:?target path required}"

    mkdir -p "$(dirname "${target_path}")"

    envsubst < "${config_template}" > "${resolved_path}"

    echo "Packaging $(basename "${target_path}")..."
    nfpm package \
        --config "${resolved_path}" \
        --packager "${packager}" \
        --target "${target_path}"
}
