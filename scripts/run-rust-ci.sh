#!/usr/bin/env bash

set -euo pipefail

# This is the single source of truth for the Cargo arguments behind the full CI
# matrix, including Linux-only profiles such as debug-all-features. GitHub
# Actions uses the full set; the local Just aggregators pass only the profile
# names that are portable to macOS.

usage() {
    cat <<'EOF'
Usage:
  scripts/run-rust-ci.sh help
  scripts/run-rust-ci.sh <command> <profile>

Commands:
  help               Show this usage information
  check              Run cargo check with the CI profile
  build              Run cargo build with the CI profile
  clippy             Run the pinned PR clippy toolchain with the CI profile
  clippy-nightly     Run the latest nightly clippy toolchain with the CI profile
  test               Run cargo-nextest with the CI profile
  benchmark-example  Run the benchmark example exercised by CI
  insert-example     Run the insert example exercised by CI

Profiles:
  debug-no-default-features
  debug-no-features (default features)
  debug-ethhash-logger
  debug-all-features (Linux-only; enables io-uring)
  maxperf-ethhash-logger
EOF
}

if [[ $# -eq 1 && $1 == help ]]; then
    usage
    exit 0
fi

if [[ $# -ne 2 ]]; then
    usage >&2
    exit 2
fi

command=$1
profile=$2

cargo_args=()
nextest_args=()
case "$profile" in
    debug-no-default-features)
        cargo_args=(--no-default-features)
        nextest_args=(--no-default-features)
        ;;
    debug-no-features)
        ;;
    debug-ethhash-logger)
        cargo_args=(--features ethhash,logger)
        nextest_args=(--features ethhash,logger)
        ;;
    debug-all-features)
        cargo_args=(--all-features)
        nextest_args=(--all-features)
        ;;
    maxperf-ethhash-logger)
        cargo_args=(--profile maxperf --features ethhash,logger)
        nextest_args=(--cargo-profile maxperf --features ethhash,logger)
        ;;
    *)
        echo "error: unknown Rust CI profile '$profile'" >&2
        usage >&2
        exit 2
        ;;
esac

# Expanding an empty array with "${arr[@]}" is an unbound-variable error
# under `set -u` on bash < 4.4 (macOS ships 3.2), so expansion sites use
# ${arr[@]+"${arr[@]}"} instead.
case "$command" in
    check)
        cargo check --frozen ${cargo_args[@]+"${cargo_args[@]}"} --workspace --all-targets
        ;;
    build)
        cargo build --frozen ${cargo_args[@]+"${cargo_args[@]}"} --workspace --all-targets
        ;;
    clippy)
        cargo +nightly-2026-07-05 clippy --locked ${cargo_args[@]+"${cargo_args[@]}"} --workspace --all-targets -- -D warnings
        ;;
    clippy-nightly)
        cargo +nightly clippy --locked ${cargo_args[@]+"${cargo_args[@]}"} --workspace --all-targets -- -D warnings
        ;;
    test)
        cargo nextest run --locked --profile ci --verbose ${nextest_args[@]+"${nextest_args[@]}"}
        ;;
    benchmark-example)
        cargo run --locked ${cargo_args[@]+"${cargo_args[@]}"} --bin benchmark -- --number-of-batches 100 --batch-size 1000 create
        ;;
    insert-example)
        cargo run --locked ${cargo_args[@]+"${cargo_args[@]}"} --example insert
        ;;
    *)
        echo "error: unknown Rust CI command '$command'" >&2
        usage >&2
        exit 2
        ;;
esac
