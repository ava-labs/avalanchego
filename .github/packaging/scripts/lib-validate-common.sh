#!/usr/bin/env bash
# Shared functions for package validation scripts.

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
