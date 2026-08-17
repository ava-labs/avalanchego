# List available recipes
default:
    ./scripts/run-just.sh --list

# Run a Rust command with a named CI profile.
ci-rust command profile:
    ./scripts/run-rust-ci.sh "{{command}}" "{{profile}}"

# Check Rust formatting as CI does.
ci-format:
    cargo fmt -- --check

# Check TODO/FIXME annotations as CI does.
ci-check-todos:
    ./scripts/check-todos.sh

# Build Rust documentation with CI's warning policy.
ci-docs:
    RUSTDOCFLAGS="-D warnings" cargo doc --locked --document-private-items --no-deps

# Lint the Markdown files selected by the shared markdownlint configuration.
ci-lint-markdown:
    markdownlint-cli2

# Fix supported Markdown lint errors in the files selected by the shared config.
fix-markdown:
    markdownlint-cli2 --fix

# Lint the Go FFI as CI does.
ci-lint-ffi:
    ./ffi/scripts/lint.sh

# Check that `go generate` leaves the FFI sources unchanged, as CI does.
ci-check-go-generate:
    #!/usr/bin/env bash
    set -euo pipefail
    # Compare repository status before and after so pre-existing local
    # modifications do not count as failures.
    before=$(git status --porcelain)
    (cd ffi && go generate)
    after=$(git status --porcelain)
    if [[ "$before" != "$after" ]]; then
        echo "error: go generate resulted in changes to tracked files. Please commit these changes." >&2
        git --no-pager diff
        exit 1
    fi

# Check for unused Rust dependencies as CI does.
ci-machete:
    cargo machete --with-metadata

# Build the Go FFI's Rust library for a hash mode.
ci-build-ffi hash_mode:
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{hash_mode}}" in
        firewood) features=() ;;
        ethhash) features=(--features ethhash,logger) ;;
        *) echo "error: unknown FFI hash mode '{{hash_mode}}'" >&2; exit 2 ;;
    esac
    # ${features[@]+…} guard: empty-array expansion errors under `set -u` on
    # the stock macOS bash 3.2.
    cargo build --locked -p firewood-ffi ${features[@]+"${features[@]}"}

# Test the Go FFI against a Rust library built for a hash mode.
ci-test-ffi hash_mode:
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{hash_mode}}" in
        firewood | ethhash) ;;
        *) echo "error: unknown FFI hash mode '{{hash_mode}}'" >&2; exit 2 ;;
    esac
    cd ffi
    GOEXPERIMENT=cgocheck2 TEST_FIREWOOD_HASH_MODE="{{hash_mode}}" go test -count=1 -race ./...

# Test a Go compatibility suite against the corresponding FFI hash mode.
ci-test-ffi-compat hash_mode:
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{hash_mode}}" in
        firewood) test_dir=ffi/tests/firewood ;;
        ethhash) test_dir=ffi/tests/eth ;;
        *) echo "error: unknown FFI hash mode '{{hash_mode}}'" >&2; exit 2 ;;
    esac
    cd "$test_dir"
    go test -count=1 -race ./...

# Run macOS-compatible CI lints.
lint:
    ./scripts/run-just.sh ci-format
    ./scripts/run-just.sh ci-check-todos
    ./scripts/run-just.sh ci-rust clippy debug-no-default-features
    ./scripts/run-just.sh ci-rust clippy debug-no-features
    ./scripts/run-just.sh ci-rust clippy debug-ethhash-logger
    ./scripts/run-just.sh ci-rust clippy debug-all-features
    ./scripts/run-just.sh ci-rust clippy maxperf-ethhash-logger
    ./scripts/run-just.sh ci-lint-markdown
    ./scripts/run-just.sh ci-docs
    ./scripts/run-just.sh ci-lint-ffi
    ./scripts/run-just.sh ci-check-go-generate
    ./scripts/run-just.sh ci-machete

# Run macOS-compatible CI unit tests (differential fuzzing remains Linux-only).
test:
    ./scripts/run-just.sh ci-rust test debug-no-default-features
    ./scripts/run-just.sh ci-rust test debug-no-features
    ./scripts/run-just.sh ci-rust test debug-ethhash-logger
    ./scripts/run-just.sh ci-rust test debug-all-features
    ./scripts/run-just.sh ci-rust test maxperf-ethhash-logger
    ./scripts/run-just.sh ci-build-ffi firewood
    ./scripts/run-just.sh ci-test-ffi firewood
    ./scripts/run-just.sh ci-test-ffi-compat firewood
    ./scripts/run-just.sh ci-build-ffi ethhash
    ./scripts/run-just.sh ci-test-ffi ethhash
    ./scripts/run-just.sh ci-test-ffi-compat ethhash

# Run every macOS-compatible pre-push check, with linting first.
prepush: lint test

# Regenerate proof wire serialization snapshots for both hash modes.
#
# Run this after any intentional change to the proof binary format (ser.rs,
# de.rs, childmask, or header). Covers ProofNode encoding, the 32-byte proof
# header, key-value pair encoding, and BatchOp encoding. The recipe writes
# snapshots for the MerkleDB (SHA-256) mode first, then for the Ethereum
# (Keccak-256, ethhash) mode. Existing snapshots are overwritten when their
# content changes.
#
# After running, review the diffs in src/proofs/snapshots/ and commit them
# alongside the format change.
snapshot-proof-nodes:
    INSTA_UPDATE=always cargo nextest run -p firewood --features logger         -E 'test(~snapshot_tests)'
    INSTA_UPDATE=always cargo nextest run -p firewood --features ethhash,logger -E 'test(~snapshot_tests)'

# Regenerate firewood-storage node serialization snapshots for both hash modes.
#
# Run this after any intentional change to Serializable impls (TrieHash,
# FreeArea, HashOrRlp) or to Node::as_bytes / Node::from_reader. The recipe
# writes snapshots for the MerkleDB (SHA-256) mode first, then for the
# Ethereum (Keccak-256, ethhash) mode. Existing snapshots are overwritten
# when their content changes.
#
# After running, review the diffs in storage/src/node/snapshots/ and commit
# them alongside the format change.
snapshot-nodes:
    INSTA_UPDATE=always cargo nextest run -p firewood-storage --features logger         -E 'test(~snapshot_tests)'
    INSTA_UPDATE=always cargo nextest run -p firewood-storage --features ethhash,logger -E 'test(~snapshot_tests)'

# Regenerate all snapshots across the workspace for both hash modes.
#
# Alias for running snapshot-proof-nodes followed by snapshot-nodes. Use this
# after a change that affects multiple snapshot suites simultaneously (e.g. a
# shared encoding primitive or a proof format change that ripples into storage).
snapshot-all: snapshot-proof-nodes snapshot-nodes

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

    GO_VERSION=$(nix develop --command bash -c "go mod edit -json | jq -r '.Go'")
    echo "go version in ffi/go.mod is ${GO_VERSION}"

    ETH_TESTS_VERSION=$(nix develop --command bash -c "cd tests/eth && go mod edit -json | jq -r '.Go'")
    echo "go version in ffi/tests/eth/go.mod is ${ETH_TESTS_VERSION}"

    if [[ "${GO_VERSION}" != "${ETH_TESTS_VERSION}" ]]; then
        echo "❌ go version in ffi/tests/eth/go.mod should be ${GO_VERSION}"
        FAILED=1
    fi

    FIREWOOD_TESTS_VERSION=$(nix develop --command bash -c "cd tests/firewood && go mod edit -json | jq -r '.Go'")
    echo "go version in ffi/tests/firewood/go.mod is ${FIREWOOD_TESTS_VERSION}"

    if [[ "${GO_VERSION}" != "${FIREWOOD_TESTS_VERSION}" ]]; then
        echo "❌ go version in ffi/tests/firewood/go.mod should be ${GO_VERSION}"
        FAILED=1
    fi

    NIX_VERSION=$(nix run .#go -- version | awk '{print $3}' | sed 's/^go//')
    echo "golang provided by ffi/flake.nix is ${NIX_VERSION}"

    if [[ "${GO_VERSION}" != "${NIX_VERSION}" ]]; then
        echo "❌ golang provided by ffi/flake/nix should be ${GO_VERSION}"
        echo "It will be necessary to update the golang.url in ffi/flake.nix to point to a SHA of"\
             "AvalancheGo whose nix/go/flake.nix provides ${GO_VERSION}."
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
        echo "  - Visit: https://nixos.org/download/" >&2
        echo "  - Or run (multi-user install): curl -L https://nixos.org/nix/install | sh -s -- --daemon" >&2
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
test-ffi-nix: test-ffi-nix-go-bindings

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

    echo "ensuring flake lock file is current for golang and rust-overlay"
    nix flake update golang rust-overlay

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

# Regenerate the git-ignored mdBook preprocessor assets (prerequisite of book builds)
book-assets:
    mdbook-mermaid install docs

# Serve the book locally with live reload
book-serve: book-assets
    mdbook serve docs --open

# Build the book and run the link checker (the book-build + linkcheck part of CI,
# not the full site assembly)
book-build: book-assets
    mdbook build docs

# List design docs by last git-commit date (oldest/stalest first).
# Freshness comes from git history — design docs carry no in-doc dates.
design-age:
    ./scripts/design-doc-age.sh

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

    # This workflow only works with a clean repo — the remote branch must match HEAD.
    if ! git rev-parse --abbrev-ref @{u} &>/dev/null 2>&1; then
        echo "error: Branch '$branch' has no upstream. Push first:" >&2
        echo "       git push -u origin $branch" >&2
        exit 1
    fi
    local_sha=$(git rev-parse HEAD)
    remote_sha=$(git rev-parse "@{u}")
    if [[ "$local_sha" != "$remote_sha" ]]; then
        echo "error: Branch '$branch' has unpushed commits — push first." >&2
        echo "       local:  $local_sha" >&2
        echo "       remote: $remote_sha" >&2
        echo "       Or set FIREWOOD_REF explicitly to benchmark a specific version." >&2
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

    # avago-runner-i4i-2xlarge-local-ssd is the dedicated Firewood runner with local SSD.
    # It has 10 replicas and each run is isolated to one replica, which keeps
    # infrastructure variance low (<1 mGAS/s for parallel runs vs 19 mGAS/s on shared infra).
    # Do not change this default without understanding the variance implications.
    : "${RUNNER:=avago-runner-i4i-2xlarge-local-ssd}"

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
    # Default timeout covers firewood-40m-41m with buffer.
    # Override for longer tests: e.g., TIMEOUT_MINUTES=600 just bench-cchain
    : "${TIMEOUT_MINUTES:=240}"
    args+=(-f timeout-minutes="$TIMEOUT_MINUTES")

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
        echo "error: Could not find workflow run after 30s. The trigger may have failed." >&2
        echo "       Check: https://github.com/ava-labs/firewood/actions/workflows/track-performance.yml" >&2
        exit 1
    fi

    echo ""
    echo "Monitor this workflow with cli: $GH run watch $run_id"
    echo " or with this URL: https://github.com/ava-labs/firewood/actions/runs/$run_id"
    echo ""
