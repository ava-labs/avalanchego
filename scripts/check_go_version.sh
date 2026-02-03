#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ERRORS=()

# Extract Go version from root go.mod (source of truth)
GO_MOD_VERSION=$(grep -E "^go [0-9]+\.[0-9]+(\.[0-9]+)?$" "$REPO_ROOT/go.mod" | awk '{print $2}')

if [[ -z "$GO_MOD_VERSION" ]]; then
    echo "ERROR: Could not extract Go version from go.mod"
    exit 1
fi

echo "Source of truth (go.mod): $GO_MOD_VERSION"

# Check MODULE.bazel (use sed for macOS compatibility - grep -oP is GNU-only)
MODULE_VERSION=$(sed -n 's/.*go_sdk\.download(version = "\([0-9.]*\)".*/\1/p' "$REPO_ROOT/MODULE.bazel" 2>/dev/null || echo "")
if [[ -z "$MODULE_VERSION" ]]; then
    ERRORS+=("MODULE.bazel: go_sdk.download version not found")
elif [[ "$GO_MOD_VERSION" != "$MODULE_VERSION" ]]; then
    ERRORS+=("MODULE.bazel: $MODULE_VERSION (expected $GO_MOD_VERSION)")
fi

# Check nix/go/default.nix (use sed for macOS compatibility)
NIX_VERSION=$(sed -n 's/.*goVersion = "\([0-9.]*\)".*/\1/p' "$REPO_ROOT/nix/go/default.nix" 2>/dev/null || echo "")
if [[ -z "$NIX_VERSION" ]]; then
    ERRORS+=("nix/go/default.nix: goVersion not found")
elif [[ "$GO_MOD_VERSION" != "$NIX_VERSION" ]]; then
    ERRORS+=("nix/go/default.nix: $NIX_VERSION (expected $GO_MOD_VERSION)")
fi

# Check graft module go.mod files
for GRAFT_MOD in graft/coreth/go.mod graft/evm/go.mod graft/subnet-evm/go.mod; do
    if [[ -f "$REPO_ROOT/$GRAFT_MOD" ]]; then
        GRAFT_VERSION=$(grep -E "^go [0-9]+\.[0-9]+(\.[0-9]+)?$" "$REPO_ROOT/$GRAFT_MOD" | awk '{print $2}')
        if [[ -z "$GRAFT_VERSION" ]]; then
            ERRORS+=("$GRAFT_MOD: Go version not found")
        elif [[ "$GO_MOD_VERSION" != "$GRAFT_VERSION" ]]; then
            ERRORS+=("$GRAFT_MOD: $GRAFT_VERSION (expected $GO_MOD_VERSION)")
        fi
    fi
done

# Report results
if [[ ${#ERRORS[@]} -gt 0 ]]; then
    echo ""
    echo "ERROR: Go version mismatch detected!"
    echo "Expected version: $GO_MOD_VERSION (from go.mod)"
    echo ""
    echo "Mismatches:"
    for err in "${ERRORS[@]}"; do
        echo "  - $err"
    done
    exit 1
fi

echo "Go version check passed: $GO_MOD_VERSION"
