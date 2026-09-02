#!/usr/bin/env bash

set -euo pipefail

# Why this exists:
# Bazel's HTTP remote-cache timeout is an inactivity timeout. A connection that
# continues to transfer bytes slowly can therefore block a build indefinitely.
# The CI jobs using the remote cache should fail within a known time and still
# have one chance to complete when the cache or its network path is unhealthy.
#
# The setup action writes remote-cache options to the file named by
# BAZEL_REMOTE_CACHE_RC and imports it from ~/.bazelrc. Removing that file
# leaves the repository, disk cache, and dependency cache unchanged, but makes
# the next Bazel invocation remote-cache-free. The fallback is therefore not a
# cold build. The file contains the cache authorization header: keep it under
# HOME, do not log its contents, and never include it in an artifact.
#
# This wrapper is for Bazel build, test, and run commands that consume cached
# action outputs. Do not use it to recover `bazel fetch` or `cquery`: fetch
# failures concern repository downloads, while cquery does not execute actions.
# If the file is absent, this is a normal local invocation and the wrapper must
# not add deadlines or retries.
remote_cache_rc="${BAZEL_REMOTE_CACHE_RC:-}"
if [[ -z "${remote_cache_rc}" || ! -f "${remote_cache_rc}" ]]; then
  exec bazelisk "$@"
fi

# Keep the cache-backed attempt near the expected CI duration. The cache-free
# retry gets longer because it may need to rebuild every action locally. Tune
# these only after measuring both paths on each CI platform; they are integer
# seconds so the Bash watchdog below works on GitHub's macOS runners.
primary_timeout_seconds="${BAZEL_CACHE_ATTEMPT_TIMEOUT_SECONDS:-900}"
fallback_timeout_seconds="${BAZEL_CACHE_FALLBACK_TIMEOUT_SECONDS:-1800}"

# GitHub's macOS runners do not provide GNU timeout. Keep this implementation
# in Bash rather than installing Nix or adding a Python dependency to every
# Bazel job. It terminates the Bazel client, not its persistent server; callers
# must run `bazelisk shutdown` before a retry.
run_with_timeout() {
  local timeout_seconds="$1"
  shift

  if ! [[ "${timeout_seconds}" =~ ^[0-9]+$ ]]; then
    echo "timeout must be an integer number of seconds: ${timeout_seconds}" >&2
    return 2
  fi

  "$@" &
  local command_pid=$!
  local timed_out_file
  timed_out_file="$(mktemp)"

  # Use a marker rather than the command's exit status to distinguish our
  # deadline from a normal Bazel failure. The watchdog owns its sleep children
  # and kills the active one when the command finishes, preventing a leftover
  # watchdog from waking up during a later command.
  (
    sleep "${timeout_seconds}" &
    sleep_pid=$!
    trap 'kill "${sleep_pid}" 2>/dev/null || true; exit 0' TERM
    wait "${sleep_pid}"
    if kill -0 "${command_pid}" 2>/dev/null; then
      printf 'timed out\n' >"${timed_out_file}"
      kill -TERM "${command_pid}" 2>/dev/null || true
      sleep 30 &
      sleep_pid=$!
      wait "${sleep_pid}"
      kill -KILL "${command_pid}" 2>/dev/null || true
    fi
  ) &
  local watchdog_pid=$!

  local status
  if wait "${command_pid}"; then
    status=0
  else
    status=$?
  fi
  kill "${watchdog_pid}" 2>/dev/null || true
  wait "${watchdog_pid}" 2>/dev/null || true

  if [[ -s "${timed_out_file}" ]]; then
    rm -f "${timed_out_file}"
    return 124
  fi
  rm -f "${timed_out_file}"
  return "${status}"
}

set +e
run_with_timeout "${primary_timeout_seconds}" bazelisk "$@"
status=$?
set -e

if (( status == 0 )); then
  exit 0
fi

if (( status == 124 )); then
  echo "Bazel command exceeded the ${primary_timeout_seconds}s remote-cache attempt deadline." >&2
else
  echo "Bazel command failed with the remote cache enabled (exit ${status})." >&2
fi

# Bazel reports both cache transport failures and ordinary build/test failures
# as a non-zero status. Do one cache-free retry for either case rather than
# matching unstable error text. This can also rerun a flaky test; the bounded
# retry is an intentional trade-off for making cache failures recoverable. If
# that trade-off stops being acceptable, narrow this to deadlines and stable
# transport errors rather than adding more retries. A second failure is
# returned to CI, so this is a recovery path, not an unbounded retry policy.
echo "Retrying once with the remote cache disabled." >&2
rm -f "${remote_cache_rc}"

# A timed-out client can leave its server running. Ask it to stop, but do not
# trust that request to complete: a wedged server can still hold the output-base
# lock. --noblock_for_lock makes the retry fail immediately in that case rather
# than waiting behind the failed command. Do not use deprecated --batch here.
run_with_timeout 30 bazelisk shutdown || true

set +e
run_with_timeout "${fallback_timeout_seconds}" bazelisk --noblock_for_lock "$@"
status=$?
set -e

if (( status == 0 )); then
  # This is intentionally a warning, not a failure: the job is correct, but
  # maintainers need a searchable signal that cache availability affected it.
  # GitHub alone interprets workflow-command syntax; local callers get stderr.
  warning_prefix="warning: "
  if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
    warning_prefix="::warning::"
  fi
  echo "${warning_prefix}Bazel succeeded after retrying with the remote cache disabled." >&2
fi
exit "${status}"
