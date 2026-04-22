# List available recipes
default:
    ./scripts/run-just.sh --list

# Build ffi with nix
build-ffi-nix: check-nix
    cd ffi && nix build

# Check if the git branch is clean
check-clean-branch:
    #!/usr/bin/env bash
    set -euo pipefail

    git add --all
    git update-index --really-refresh >> /dev/null

    # Show the status of the working tree.
    git status --short

    # Exits if any uncommitted changes are found.
    git diff-index --quiet HEAD

# Check if the FFI flake is up-to-date (requires clean git tree)
check-ffi-flake: check-nix
    #!/usr/bin/env bash
    set -euo pipefail
    ./scripts/run-just.sh update-ffi-flake
    ./scripts/run-just.sh check-clean-branch

# Check if the golang version is set consistently (requires clean git tree)
check-golang-version: check-nix
    #!/usr/bin/env bash
    set -euo pipefail

    # Exit only at the end if any of the checks set FAILED=1
    FAILED=

    cd ffi

    TOOLCHAIN_VERSION=$(nix develop --command bash -c "go mod edit -json | jq -r '.Toolchain'")
    echo "toolchain version in ffi/go.mod is ${TOOLCHAIN_VERSION}"

    ETH_TESTS_VERSION=$(nix develop --command bash -c "cd tests/eth && go mod edit -json | jq -r '.Toolchain'")
    echo "toolchain version in ffi/tests/eth/go.mod is ${ETH_TESTS_VERSION}"

    if [[ "${TOOLCHAIN_VERSION}" != "${ETH_TESTS_VERSION}" ]]; then
        echo "❌ toolchain version in ffi/tests/eth/go.mod should be ${TOOLCHAIN_VERSION}"
        FAILED=1
    fi

    FIREWOOD_TESTS_VERSION=$(nix develop --command bash -c "cd tests/firewood && go mod edit -json | jq -r '.Toolchain'")
    echo "toolchain version in ffi/tests/firewood/go.mod is ${FIREWOOD_TESTS_VERSION}"

    if [[ "${TOOLCHAIN_VERSION}" != "${FIREWOOD_TESTS_VERSION}" ]]; then
        echo "❌ toolchain version in ffi/tests/firewood/go.mod should be ${TOOLCHAIN_VERSION}"
        FAILED=1
    fi

    NIX_VERSION=$(nix run .#go -- version | awk '{print $3}')
    echo "golang provided by ffi/flake.nix is ${NIX_VERSION}"

    if [[ "${TOOLCHAIN_VERSION}" != "${NIX_VERSION}" ]]; then
        echo "❌ golang provided by ffi/flake/nix should be ${TOOLCHAIN_VERSION}"
        echo "It will be necessary to update the golang.url in ffi/flake.nix to point to a SHA of"\
             "AvalancheGo whose nix/go/flake.nix provides ${TOOLCHAIN_VERSION}."
    fi

    if [[ -n "${FAILED}" ]]; then
        exit 1
    fi

# Check if nix is installed
check-nix:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v nix &> /dev/null; then
        echo "Error: 'nix' is not installed." >&2
        echo "" >&2
        echo "To install nix:" >&2
        echo "  - Visit: https://github.com/DeterminateSystems/nix-installer" >&2
        echo "  - Or run: curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install" >&2
        exit 1
    fi

# Adds go workspace for user experience consistency
setup-go-workspace:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -f "go.work" ]; then
        rm go.work go.work.sum
    fi
    go work init ./ffi ./ffi/tests/eth ./ffi/tests/firewood

# Run all checks of ffi built with nix
test-ffi-nix: test-ffi-nix-build-equivalency test-ffi-nix-go-bindings

# Test ffi build equivalency between nix and cargo
test-ffi-nix-build-equivalency: check-nix
    #!/usr/bin/env bash
    set -euo pipefail

    echo "Testing ffi build equivalency between nix and cargo"

    bash -x ./ffi/test-build-equivalency.sh

# Test golang ffi bindings using the nix-built artifacts
test-ffi-nix-go-bindings: build-ffi-nix
    #!/usr/bin/env bash
    set -euo pipefail

    echo "running ffi tests against bindings built by nix..."

    cd ffi

    # Need to capture the flake path before changing directories to
    # result/ffi because `result` is a nix store symlink so ../../
    # won't resolve to the ffi path containing the flake.
    FLAKE_PATH="$PWD"

    # This runs golang outside a nix shell to validate viability
    # without the env setup performed by a nix shell
    GO="nix run $FLAKE_PATH#go"

    cd result/ffi

    # - cgocheck2 is expensive but provides complete pointer checks
    # - use hash mode ethhash since the flake builds with `--features ethhash,logger`
    GOEXPERIMENT=cgocheck2 TEST_FIREWOOD_HASH_MODE=ethhash ${GO} test ./...

# Ensure the FFI flake is up-to-date
update-ffi-flake: check-nix
    #!/usr/bin/env bash
    set -euo pipefail
    cd ffi

    echo "ensuring flake lock file is current for golang"
    nix flake update golang

    echo "checking for a consistent golang verion"
    ../scripts/run-just.sh check-golang-version

# RELEASE PREP: update all rust dependencies
release-step-update-rust-dependencies:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Checking that cargo-edit is installed and up-to-date..."
    cargo install --locked cargo-edit

    echo "Upgrading all cargo dependencies in the workspace..."
    cargo upgrade
    # MAY FAIL: temporarily comment out if resolving updates requires significant code changes
    echo "Upgrading all incompatible cargo dependencies in the workspace..." >&2
    echo "NOTICE: This step may fail if incompatible upgrades require code changes." >&2
    cargo upgrade --incompatible
    echo "Updating Cargo.lock with upgraded dependencies..."
    cargo update --verbose

    echo "Executing tests to ensure upgrades did not break anything..."
    cargo test --workspace --all-targets -F logger
    cargo test --workspace --all-targets -F ethhash,logger

# RELEASE PREP: refresh changelog
release-step-refresh-changelog tag:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Checking that git-cliff is installed and up-to-date..."
    cargo install --locked git-cliff

    echo "Generating changelog..."
    git cliff -o CHANGELOG.md --tag "{{tag}}"

# Run a C-Chain reexecution benchmark
# Triggers Firewood's track-performance.yml which then triggers AvalancheGo.
# This ensures results appear in Firewood's workflow summary and get published
# to GitHub Pages for the current branch.
#
# Note: Changes must be pushed to the remote branch for the workflow to use them.
#
# By default, uses HEAD of your current branch to build Firewood.
# If you want to benchmark a specific version (e.g., a release tag), set FIREWOOD_REF explicitly:
#   FIREWOOD_REF=v0.1.0 TEST=firewood-101-250k just bench-cchain
#
# Examples:
#   TEST=firewood-101-250k just bench-cchain
#   FIREWOOD_REF=v0.1.0 TEST=firewood-33m-40m just bench-cchain
#   START_BLOCK=1 END_BLOCK=100 BLOCK_DIR_SRC=cchain-mainnet-blocks-200-ldb just bench-cchain
bench-cchain:
    #!/usr/bin/env -S bash -euo pipefail
    
    # Prevent accidental runs from main (would pollute official bench/ data)
    branch=$(git rev-parse --abbrev-ref HEAD)
    if [[ "$branch" == "main" ]]; then
        echo "error: Cannot run bench-cchain from main branch" >&2
        echo "       Main branch benchmarks go to bench/ (official history) — use scheduled workflows only." >&2
        echo "       Feature branch benchmarks go to dev/bench/{branch}/ — create a branch first." >&2
        exit 1
    fi

    # AVALANCHEGO_REF must be a branch/tag name, not a commit SHA (GitHub API limitation)
    if [[ "${AVALANCHEGO_REF:-}" =~ ^[0-9a-fA-F]{7,40}$ ]]; then
        echo "error: AVALANCHEGO_REF looks like a commit SHA: $AVALANCHEGO_REF" >&2
        echo "       GitHub's workflow_dispatch API only accepts branch/tag names, not commit SHAs." >&2
        echo "       Use a branch name (e.g., 'master') or tag instead." >&2
        exit 1
    fi
    
    # Resolve gh CLI
    if command -v gh &>/dev/null; then
        GH=gh
    elif command -v nix &>/dev/null; then
        GH="nix run ./ffi#gh --"
    else
        echo "error: 'gh' CLI not found. Install it or use 'nix develop ./ffi'" >&2
        exit 1
    fi
    
    # Validate: need either test name OR custom block params
    if [[ -z "${TEST:-}" && -z "${START_BLOCK:-}" ]]; then
        echo "error: Provide TEST or set START_BLOCK, END_BLOCK, BLOCK_DIR_SRC" >&2
        echo "" >&2
        echo "Predefined tests:" >&2
        echo "  firewood-101-250k, firewood-33m-33m500k, firewood-33m-40m" >&2
        echo "  firewood-archive-101-250k, firewood-archive-33m-33m500k, firewood-archive-33m-40m" >&2
        echo "" >&2
        echo "Custom mode example:" >&2
        echo "  START_BLOCK=1 END_BLOCK=100 BLOCK_DIR_SRC=cchain-mainnet-blocks-200-ldb just bench-cchain" >&2
        exit 1
    fi
    
    : "${RUNNER:=avalanche-avalanchego-runner-2ti}"
    
    # Build workflow args
    args=(-f runner="$RUNNER")
    [[ -n "${TEST:-}" ]] && args+=(-f test="$TEST")
    [[ -n "${FIREWOOD_REF:-}" ]] && args+=(-f firewood="$FIREWOOD_REF")
    [[ -n "${LIBEVM_REF:-}" ]] && args+=(-f libevm="$LIBEVM_REF")
    [[ -n "${AVALANCHEGO_REF:-}" ]] && args+=(-f avalanchego="$AVALANCHEGO_REF")
    [[ -n "${CONFIG:-}" ]] && args+=(-f config="$CONFIG")
    [[ -n "${START_BLOCK:-}" ]] && args+=(-f start-block="$START_BLOCK")
    [[ -n "${END_BLOCK:-}" ]] && args+=(-f end-block="$END_BLOCK")
    [[ -n "${BLOCK_DIR_SRC:-}" ]] && args+=(-f block-dir-src="$BLOCK_DIR_SRC")
    [[ -n "${CURRENT_STATE_DIR_SRC:-}" ]] && args+=(-f current-state-dir-src="$CURRENT_STATE_DIR_SRC")
    [[ -n "${TIMEOUT_MINUTES:-}" ]] && args+=(-f timeout-minutes="$TIMEOUT_MINUTES")
    
    [[ -n "${TEST:-}" ]] && echo "==> Test: $TEST"
    [[ -n "${START_BLOCK:-}" ]] && echo "==> Custom: blocks $START_BLOCK-${END_BLOCK:-?}"
    echo "==> Runner: $RUNNER"
    
    # Record time before triggering to find our run (avoid race conditions)
    trigger_time=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    
    $GH workflow run track-performance.yml --ref "$branch" "${args[@]}"
    
    # Poll for workflow registration (runs created after trigger_time)
    echo ""
    echo "Polling for workflow to register..."
    for i in {1..30}; do
        sleep 1
        run_id=$($GH run list --workflow=track-performance.yml --limit=10 --json databaseId,createdAt \
            --jq "[.[] | select(.createdAt > \"$trigger_time\")] | .[-1].databaseId // empty")
        [[ -n "$run_id" ]] && break
    done
    
    if [[ -z "$run_id" ]]; then
        echo "warning: Could not find run ID. Check manually at:"
        echo "  https://github.com/ava-labs/firewood/actions/workflows/track-performance.yml"
        exit 0
    fi
    
    echo ""
    echo "Monitor this workflow with cli: $GH run watch $run_id"
    echo " or with this URL: https://github.com/ava-labs/firewood/actions/runs/$run_id"
    echo ""
