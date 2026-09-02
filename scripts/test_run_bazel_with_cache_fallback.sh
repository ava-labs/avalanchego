#!/usr/bin/env bash

set -euo pipefail

# This is a policy test, not a remote-cache integration test. The fake Bazelisk
# treats the presence of BAZEL_REMOTE_CACHE_RC as the cache-enabled state and
# records every invocation. That lets the test verify the retry boundary and
# cleanup behavior without depending on a cache service or a Bazel binary. It
# does not verify Bazel rc parsing, HTTP-cache authentication, or cache-server
# behavior; cover those separately with a CI integration check if this wrapper
# changes or the cache service changes.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runner="${repo_root}/scripts/run_bazel_with_cache_fallback.sh"
bash_bin="$(command -v bash)"

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

bin_dir="${workdir}/bin"
home_dir="${workdir}/home"
mkdir -p "${bin_dir}" "${home_dir}"

# The wrapper deliberately invokes Bazelisk again after removing the rc file.
# Keep the fake small: it records whether that file exists, optionally models a
# stalled cache download, and returns independently configured primary and
# fallback statuses.
cat >"${bin_dir}/bazelisk" <<EOF
#!${bash_bin}
set -euo pipefail

cache_state=disabled
if [[ -f "\${BAZEL_REMOTE_CACHE_RC}" ]]; then
  cache_state=enabled
fi
printf '%s %s\n' "\$*" "\${cache_state}" >>"\${BAZEL_FALLBACK_TEST_LOG}"

if [[ "\$1" == "shutdown" ]]; then
  exit 0
fi

if [[ "\${cache_state}" == "enabled" ]]; then
  if [[ -n "\${BAZEL_FALLBACK_TEST_PRIMARY_SLEEP_SECONDS:-}" ]]; then
    sleep "\${BAZEL_FALLBACK_TEST_PRIMARY_SLEEP_SECONDS}"
  fi
  exit "\${BAZEL_FALLBACK_TEST_PRIMARY_STATUS:-1}"
fi
exit "\${BAZEL_FALLBACK_TEST_FALLBACK_STATUS:-0}"
EOF
chmod +x "${bin_dir}/bazelisk"

assert_stderr_contains() {
  local expected="$1"
  if ! grep -Fq "${expected}" "${workdir}/stderr"; then
    echo "stderr did not contain: ${expected}" >&2
    exit 1
  fi
}

assert_stderr_not_contains() {
  local unexpected="$1"
  if grep -Fq "${unexpected}" "${workdir}/stderr"; then
    echo "stderr unexpectedly contained: ${unexpected}" >&2
    exit 1
  fi
}

assert_log() {
  local expected="$1"
  local actual
  actual="$(<"${workdir}/log")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "expected log:" >&2
    printf '%s\n' "${expected}" >&2
    echo "actual log:" >&2
    printf '%s\n' "${actual}" >&2
    exit 1
  fi
}

# Use short deadlines only in this test. Production defaults live in the
# wrapper and are deliberately much longer than a healthy CI build.
run_runner() {
  PATH="${bin_dir}:${PATH}" \
    HOME="${home_dir}" \
    BAZEL_REMOTE_CACHE_RC="${home_dir}/remote-cache.bazelrc" \
    BAZEL_FALLBACK_TEST_LOG="${workdir}/log" \
    BAZEL_CACHE_ATTEMPT_TIMEOUT_SECONDS=1 \
    BAZEL_CACHE_FALLBACK_TIMEOUT_SECONDS=1 \
    "${bash_bin}" "${runner}" build //main:avalanchego
}

# Cache failures use the same non-zero status as ordinary Bazel failures. The
# first case establishes the recovery contract: remove only the remote-cache
# rc file, shut down the potentially stuck server, and retry once cache-free.
printf 'build --remote_cache=https://cache.example\n' >"${home_dir}/remote-cache.bazelrc"
: >"${workdir}/log"
GITHUB_ACTIONS=true run_runner 2>"${workdir}/stderr"
assert_log $'build //main:avalanchego enabled\nshutdown disabled\n--noblock_for_lock build //main:avalanchego disabled'
assert_stderr_contains '::warning::Bazel succeeded after retrying with the remote cache disabled.'

# Local and cache-disabled CI use the wrapper too. They must run once without
# a deadline or retry, so this mechanism cannot change their normal behavior.
rm -f "${home_dir}/remote-cache.bazelrc"
: >"${workdir}/log"
run_runner
assert_log 'build //main:avalanchego disabled'

# A successful retry is the only success condition. Do not hide a deterministic
# build or test failure merely because the remote-cache attempt also failed.
printf 'build --remote_cache=https://cache.example\n' >"${home_dir}/remote-cache.bazelrc"
: >"${workdir}/log"
if BAZEL_FALLBACK_TEST_FALLBACK_STATUS=42 run_runner; then
  echo "expected cache-free retry to fail" >&2
  exit 1
fi
assert_log $'build //main:avalanchego enabled\nshutdown disabled\n--noblock_for_lock build //main:avalanchego disabled'

# HTTP progress can prevent Bazel's own remote timeout from firing. This case
# verifies the independent deadline: the primary process sleeps for five
# seconds but must be terminated and retried well before that completes.
printf 'build --remote_cache=https://cache.example\n' >"${home_dir}/remote-cache.bazelrc"
: >"${workdir}/log"
start_time="$(date +%s)"
BAZEL_FALLBACK_TEST_PRIMARY_SLEEP_SECONDS=5 run_runner 2>"${workdir}/stderr"
elapsed_seconds=$(( $(date +%s) - start_time ))
if (( elapsed_seconds >= 4 )); then
  echo "timed-out cache attempt took ${elapsed_seconds}s" >&2
  exit 1
fi
assert_log $'build //main:avalanchego enabled\nshutdown disabled\n--noblock_for_lock build //main:avalanchego disabled'
assert_stderr_contains 'warning: Bazel succeeded after retrying with the remote cache disabled.'
assert_stderr_not_contains '::warning::'

echo "bazel cache fallback tests passed"
