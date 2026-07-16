# CI disk space

This document explains the repository's shared CI disk-space policy for jobs
that are sensitive to runner disk exhaustion.

For general CI conventions, including action pinning policy, see
[CI](./ci.md). For general guidance on when CI should use stable task
entrypoints, see [Tasks](./tasks.md).

## Table of contents

- [Overview](#overview)
- [Usage](#usage)
- [Conceptual model](#conceptual-model)
  - [Why this policy exists](#why-this-policy-exists)
  - [Current scope](#current-scope)
  - [Threshold policy](#threshold-policy)
  - [Why the check uses /](#why-the-check-uses-)
  - [Why cleanup is conditional](#why-cleanup-is-conditional)
  - [Why cleanup is Linux-only](#why-cleanup-is-linux-only)
  - [Check, diagnostics, and cleanup are separate](#check-diagnostics-and-cleanup-are-separate)
  - [Diagnostics are curated and bounded](#diagnostics-are-curated-and-bounded)
  - [Low-space and failure-triggered diagnostics](#low-space-and-failure-triggered-diagnostics)
  - [Post-cleanup logging behavior](#post-cleanup-logging-behavior)
- [Maintainer guidance](#maintainer-guidance)
- [References](#references)

## Overview

Some GitHub-hosted runners used by this repo arrive with materially different
amounts of free disk even for the same `runs-on` label. The policy here is to
measure live free space, reclaim space only when needed, and emit bounded
runner diagnostics for the CI job classes that have historically hit ENOSPC.

The current shared implementation centers on:

- `scripts/check_ci_disk_space.sh` for threshold checks
- `scripts/log_ci_disk_state.sh` for bounded diagnostics
- `./.github/actions/ensure-disk-space` for shared check/cleanup/re-check orchestration
- thin wrapper actions that combine preflight, command execution, and failure diagnostics for specific job classes
- `./.github/actions/run-monitored-tmpnet-cmd` for monitored tmpnet jobs whose disk behavior is derived from `setup_bazel` and `runtime`

## Usage

Use this document when you are:

- changing `./.github/actions/ensure-disk-space`
- changing one of the wrapper actions that runs protected commands
- wiring disk protection into another CI job
- changing the threshold or cleanup policy
- expanding or narrowing the curated diagnostics
- validating low-space branches locally or in CI

Normal repo entrypoints include:

- `task check-ci-disk-space`
- `task log-ci-disk-state-bazel`
- `task log-ci-disk-state-docker`
- `task log-ci-disk-state-generic`

## Conceptual model

### Why this policy exists

The repo has seen disk failures in CI where the relevant bottleneck was host
root-disk pressure, including Docker-backed image-building jobs and Bazel jobs.
Runner labels alone were not a reliable predictor of usable free space, so the
policy measures disk state live instead of inferring health from `runs-on`.

### Current scope

The initial scope is intentionally limited to the known failure classes in this
repo:

- Bazel CI jobs, via `./.github/actions/setup-bazel` and Bazel-aware wrappers
- Linux image-building jobs
- Linux kind/Docker-heavy jobs that fit the repo's current image-building
  pressure pattern, including monitored tmpnet jobs with `runtime: kube`

This is not yet a blanket policy for every workflow.

### Threshold policy

The current threshold is a single minimum free-space value:

- `20G` on `/`

Behavior:

1. if free disk is `>= 20G`, continue without cleanup
2. if free disk is `< 20G`, log low-space diagnostics and try cleanup
3. re-check free disk after cleanup
4. if the runner is still below threshold, keep the post-cleanup log output and continue

Low disk is treated as runner health information, not as an automatic proof
that the job cannot succeed.

### Why the check uses /

The known Linux failures in this repo have been driven by storage ultimately
backed by the root filesystem, including Docker storage under
`/var/lib/docker`. The policy therefore uses free space on `/` as the first
shared health signal.

Revisit this only if future failures show pressure on a distinct non-root mount
or if runner storage layout changes materially.

### Why cleanup is conditional

Cleanup is not run on every job by default. The repo only reclaims space when a
runner starts below the threshold.

That keeps healthy-runner logs quieter and avoids deleting useful runner state
when the job already has enough free disk.

### Why cleanup is Linux-only

The current cleanup backend is Linux-only, and the repo's observed ENOSPC
failures so far have been Linux failures. Diagnostics can still be useful on
other platforms, but aggressive runner cleanup is currently only implemented
for Linux.

### Check, diagnostics, and cleanup are separate

The implementation keeps three responsibilities separate:

- `scripts/check_ci_disk_space.sh` checks free space and reports whether the
  runner is below threshold
- `scripts/log_ci_disk_state.sh` emits bounded diagnostics for curated path
  groups
- `./.github/actions/ensure-disk-space` orchestrates shared check, optional
  cleanup, and re-check behavior

Higher-level wrapper actions then compose that shared preflight with command
execution and failure diagnostics for particular job classes.

Preserve that split when changing this area. The check script should stay small
and easy to validate. Diagnostics should remain reusable outside the cleanup
path. The shared action should stay focused on runner-health orchestration, and
wrappers should own command-specific before/run/after-failure behavior.

### Diagnostics are curated and bounded

The diagnostics script does not try to log every possible path on the runner.
It logs curated groups that have been useful for the failure classes observed
in this repo:

- `generic` for root-disk state
- `bazel` for Bazel-related cache paths
- `docker` for Docker/image-build state

If a future failure shows that an important disk consumer is not visible, add
it to the relevant curated group instead of turning the script into an
unbounded whole-runner dump.

### Low-space and failure-triggered diagnostics

Diagnostics are emitted in two situations:

- when a protected job starts below threshold
- when an in-scope protected job fails

For monitored tmpnet jobs, the action derives the failure-diagnostics behavior
from the real job semantics instead of from an extra diagnostics selector:

- `setup_bazel: true` implies Bazel failure diagnostics
- `runtime: kube` implies Docker-oriented failure diagnostics
- when both apply, both diagnostic views are relevant

This avoids very noisy always-on logging on healthy runs while still preserving
useful evidence when a runner starts unhealthy or a Docker/Bazel-heavy job
fails later.

### Post-cleanup logging behavior

When cleanup runs, the action performs a second threshold check and relies on
that script's normal log output to show the post-cleanup state.

The current policy does not emit a GitHub Actions warning annotation or step
summary just because the runner remains below threshold. The goal is to keep
this shared action focused on measurement, conditional cleanup, and concise
logging.

## Maintainer guidance

When changing this policy, preserve these invariants:

- live measurement decides whether cleanup is needed
- the threshold check stays focused on `/`
- cleanup remains conditional rather than unconditional
- the check script remains independently testable
- diagnostics remain curated and bounded
- general CI action-pinning policy stays documented in [CI](./ci.md) rather
  than being redefined here

Current validation strategy:

- use `CI_FORCE_FREE_GB` with `task check-ci-disk-space` to exercise healthy
  and low-space paths deterministically
- use the `force-free-gb` and `force-free-gb-after-cleanup` test-only inputs on
  `./.github/actions/ensure-disk-space` when validating its branches in CI
- use the stable logging tasks to inspect the current curated diagnostics
  groups locally
- run `task lint-action` after workflow or action changes

Evidence that would justify revisiting the current design includes:

- repeated low-space cleanup runs on jobs that still complete comfortably,
  suggesting `20G` is too conservative
- repeated ENOSPC failures on jobs that started above `20G`, suggesting the
  threshold is too low for the protected workloads
- repeated low-space incidents in job classes outside the current scope
- runner-storage changes that make `/` an inadequate shared signal
- cleanup behavior from the current Linux backend becoming ineffective or hard
  to trust

## References

- [CI](./ci.md)
- [Tasks](./tasks.md)
- [Bazel](./bazel.md)
- [`scripts/check_ci_disk_space.sh`](../scripts/check_ci_disk_space.sh)
- [`scripts/log_ci_disk_state.sh`](../scripts/log_ci_disk_state.sh)
- [`./.github/actions/ensure-disk-space/action.yml`](../.github/actions/ensure-disk-space/action.yml)
- [`./.github/actions/run-bazel-with-disk-space/action.yml`](../.github/actions/run-bazel-with-disk-space/action.yml)
- [`./.github/actions/run-docker-with-disk-space/action.yml`](../.github/actions/run-docker-with-disk-space/action.yml)
- [`./.github/actions/run-monitored-tmpnet-cmd/action.yml`](../.github/actions/run-monitored-tmpnet-cmd/action.yml)
