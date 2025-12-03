#!/usr/bin/env bash

set -euo pipefail

# ==== Generating Expected Diff ====
# 1. Review the changes from AvalancheGo and apply them if necessary to our local file.
#    https://github.com/ava-labs/avalanchego/commits/master/.golangci.yml
# 2. Update the diff file by running:
#    .github/scripts/verify_golangci_yaml_changes.sh update

declare -r golangci_yaml="ffi/.golangci.yaml"
declare -r upstream_yaml=".github/.golangci.yaml"
declare -r expected_patch=".github/.golangci.yaml.patch"
declare -r upstream_url="https://raw.githubusercontent.com/ava-labs/avalanchego/refs/heads/master/.golangci.yml"
declare -r history_url="https://github.com/ava-labs/avalanchego/commits/master/.golangci.yml"

function @die() {
    local -r code="$1"
    local -r message="$2"

    echo "Error: $message" >&2
    exit "$code"
}

function @usage() {
    cat <<EOF
Usage: $0 [apply|check|update]

# Commands

   apply: Download and attempt to apply the patch automatically.
          NOTE: This may not work if the patch fails to apply cleanly. If so,
          please review the changes manually.
   check: Check if '$golangci_yaml' has unexpected changes from AvalancheGo.
          This is the default if no command is provided.
  update: Update the expected changes in '$expected_patch' with the current
          diff. Run this after manually applying changes to '$golangci_yaml' to
          keep the expected patch up to date.

# Exit Codes

  0: Command executed successfully (if 'check' command, no unexpected changes).
  1: Usage error.
  2: Failed to download upstream .golangci.yaml.
  3: Failed to generate patch; '$golangci_yaml' has no differences from '$upstream_yaml'.
  4: (on apply) Failed to apply the patch from '$expected_patch'.
  5: (on check) '$golangci_yaml' has unexpected changes from AvalancheGo.
EOF

    exit 1
}

function @download-upstream() {
    echo "Downloading upstream .golangci.yaml from '$upstream_url'..."
    if ! curl -fsSL -o "$upstream_yaml" "$upstream_url"; then
        @die 2 "Failed to download upstream .golangci.yaml from '$upstream_url'"
    fi
}

function @generate-patch() {
    local -r dest="$1"
    if diff -Nau "$upstream_yaml" "$golangci_yaml" >"$dest" 2>/dev/null; then
        @die 3 "'$golangci_yaml' has no differences '$upstream_yaml'; this is unexpected! At least package name must be different."
    fi
}

function @apply-patch-to-upstream() {
    if ! patch -t "$upstream_yaml" "$expected_patch"; then
        @die 4 "Failed to apply the patch from $expected_patch. Please review the changes manually."
    fi
}

function @apply() {
    local backup
    backup=$(mktemp)
    trap 'rm -f "$backup"' EXIT

    # make a copy of the upstream yaml before applying the patch so we can refresh the patch file later
    cp -f "$upstream_yaml" "$backup"
    @apply-patch-to-upstream

    # We cleanly applied the patch, so we can now replace the local file with the patched version
    cp -f "$upstream_yaml" "$golangci_yaml"
    cp -f "$backup" "$upstream_yaml"
    echo "Successfully applied the patch from $expected_patch to $golangci_yaml"

    # refresh the patch so `check` apply later can use it
    @update
}

function @check() {
    @apply-patch-to-upstream

    local patch
    patch=$(mktemp)
    trap 'rm -f "$patch"' EXIT

    if diff -Nau "$upstream_yaml" "$golangci_yaml" >"$patch" 2>&1; then
        echo "'$golangci_yaml' is up to date with AvalancheGo's .golangci.yaml."
        exit 0
    fi

    {
        echo "'$golangci_yaml' has unexpected changes from AvalancheGo."
        echo "View the upstream changes at: $history_url and apply them if necessary."
        echo ""
        echo "Current changes:"
        cat "$patch"
    } >&2

    exit 5
}

function @update() {
    @generate-patch "$expected_patch"
    echo "Updated expected changes in $expected_patch"
    exit 0
}

case "${1:-check}" in
apply | check | update)
    # make sure we are in the root of the repository
    cd "$(git rev-parse --show-toplevel)"

    @download-upstream

    case "${1:-check}" in
    apply) @apply ;;
    check) @check ;;
    update) @update ;;
    esac

    ;;
*) @usage ;;
esac
