#!/usr/bin/env bash
# Shared functions for package validation scripts.
# Sourced by validate-rpm.sh.

# Detect host architecture for the given package format.
# Sets PACKAGE_ARCH (global) if not already set by the caller.
#
# Args: format ("RPM")
detect_host_arch() {
    local format="${1:?format required}"
    if [[ -n "${PACKAGE_ARCH:-}" ]]; then
        return  # already set by caller
    fi
    local arch
    arch=$(uname -m)
    case "${format}" in
        RPM)
            case "${arch}" in
                x86_64) PACKAGE_ARCH="x86_64" ;;
                arm64)  PACKAGE_ARCH="aarch64" ;;
                *)      PACKAGE_ARCH="${arch}" ;;
            esac
            ;;
    esac
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
