# Test workflows

## Overview

This document explains how unit tests are split across PR, pre-merge, and
scheduled workflows:

- **PR runs** provide the fastest useful feedback.
- **Pre-merge runs** add stronger validation before merge.
- **Scheduled runs** provide broader platform coverage and stronger test
  coverage.

The main goals are:

- keep default PR feedback fast
- avoid duplicate coverage between Bazel and native Go workflows on PRs
- preserve broader validation before merge and on a schedule
- make the expanded coverage reproducible on demand when needed

## Usage

### Default PR coverage

Default PR coverage uses the Bazel workflow for unit tests:

- Bazel unit tests run on `linux-amd64/24.04`
- Bazel unit tests use the `fast` mode
  - no race detection
  - no shuffle

The native Go `Unit` job in `ci.yml` is skipped on PR runs to avoid duplicate
unit-test coverage.

The graft-module Go test jobs are also skipped on default PRs:

- `coreth-ci.yml`
- `subnet-evm-ci.yml`
- `evm-ci.yml`

### `ci/pre-merge`

Applying the `ci/pre-merge` label upgrades a PR to the same test coverage
used before merge:

- enables the native Go `Unit` job in `ci.yml`
- enables the Go test jobs in:
  - `coreth-ci.yml`
  - `subnet-evm-ci.yml`
  - `evm-ci.yml`
- expands Bazel unit coverage to:
  - `linux-amd64/24.04`
  - `darwin-arm64`
- runs all enabled Go unit-test jobs on:
  - `linux-amd64/24.04`
  - `darwin-arm64`
- switches Bazel unit tests from `fast` to `race`
  - enables race detection
  - keeps shuffle off
- runs the enabled Go test jobs with race detection on and shuffle off

This label lets contributors reproduce pre-merge coverage on a PR.

### `ci/all-platforms`

Applying the `ci/all-platforms` label runs the scheduled workflow on a PR.
This helps validate workflow changes before merge and reproduce failures that
otherwise appear only in scheduled runs.

That workflow runs:

- Bazel unit tests on:
  - `linux-amd64/24.04`
  - `linux-amd64/22.04`
  - `linux-arm64/24.04`
  - `linux-arm64/22.04`
  - `darwin-arm64`
- native Go unit tests on the same platform set
- Bazel e2e on `linux-arm64/24.04`
- the `race-shuffle` test mode for unit tests

### Scheduled runs

The scheduled workflow restores broader coverage without making every PR
expensive.

Scheduled runs:

- run both Bazel and native Go unit tests
- cover all supported CI platforms listed above
- run Bazel e2e on `linux-arm64/24.04`
- keep race detection and shuffle enabled

The graft-module workflows also have scheduled and manually dispatched test
coverage:

- `coreth-ci.yml`
- `subnet-evm-ci.yml`
- `evm-ci.yml`

For those workflows, scheduled/manual unit tests run on:

- `linux-amd64/22.04`
- `linux-amd64/24.04`
- `darwin-arm64`

and use race detection with shuffle enabled.

### Manual validation

The scheduled unit-test workflow also supports `workflow_dispatch`, so it can
be run manually against a branch when broader validation is needed. The same is
true for the graft-module workflows listed above.

## Conceptual model

### Why Bazel owns default PR unit coverage

PR unit testing defaults to Bazel so PRs use one fast test workflow instead
of running duplicate Bazel and Go unit tests.

### Why the native Go unit path still exists

The Go test workflow outside Bazel is still useful. It checks a different
path and helps ensure Go tests still work for developers who do not use Bazel.
Keeping it in pre-merge and scheduled runs helps catch issues that one test
workflow alone might miss.

### Why the Bazel workflows stay together

The Bazel CI jobs share a setup phase that prepares dependencies and cache
state for later Bazel jobs. Because GitHub Actions `needs` only works within a
single workflow, those jobs stay together.

That is why the Bazel workflow has a single required summary job.

## Maintenance notes

### Test caching affects the test modes

Part of this split comes from how the Go test cache behaves:

- `-shuffle=on` uses a time-based seed, so it prevents cache hits
- `-race` results are not reused by the Go test cache

That leads to three useful modes:

- **fast**
  - no race
  - no shuffle
  - used for default PR Bazel unit tests
- **race**
  - race on
  - shuffle off
  - used for pre-merge coverage
- **race-shuffle**
  - race on
  - shuffle on
  - used for scheduled coverage

### Required-job behavior

The required summary jobs allow `skipped` only for jobs that are optional
in that workflow mode.

The workflow files define which jobs are optional. When changing
conditional execution in [`bazel-ci.yml`](./bazel-ci.yml), [`ci.yml`](./ci.yml),
[`coreth-ci.yml`](./coreth-ci.yml), [`subnet-evm-ci.yml`](./subnet-evm-ci.yml),
or [`evm-ci.yml`](./evm-ci.yml), update the matching required-summary logic in
the same change.

[`unit-tests-scheduled.yml`](./unit-tests-scheduled.yml) is informational
rather than a required PR gate. The `ci/all-platforms` label lets contributors
opt into that broader coverage on a PR when they want results that normally
appear only in scheduled runs, but its results are not part of the required
checks.

Do not expand that behavior casually. If another job can be skipped, update
the allowlist in the same change.

### Local reproduction uses stable task names

Workflow test execution should go through stable `task` names that also work
locally.

For Bazel unit jobs, workflows should use the public no-argument task names,
such as `bazel-test-fast`, `bazel-test-race`, and `bazel-test-race-shuffle`.

The same applies to the Go test jobs, which use stable public task targets
such as `test-unit-race`, `build-test-race`, and `build-test-race-shuffle`.

### When to revisit this design

Revisit the split if any of these assumptions change:

- default PR runtime is no longer a significant concern
- scheduled coverage no longer benefits enough from race detection and shuffle
  to justify the extra runtime and lower cache hit rate
- scheduled-only failures become common enough that `workflow_dispatch` and
  `ci/all-platforms` are insufficient
- the Bazel and non-Bazel Go test paths stop providing meaningfully distinct
  validation
- non-unit Bazel coverage also needs to be included in scheduled runs

## References

- [`bazel-ci-matrix.yml`](./bazel-ci-matrix.yml)
- [`bazel-ci.yml`](./bazel-ci.yml)
- [`ci.yml`](./ci.yml)
- [`unit-tests-scheduled.yml`](./unit-tests-scheduled.yml)
- [`coreth-ci.yml`](./coreth-ci.yml)
- [`subnet-evm-ci.yml`](./subnet-evm-ci.yml)
- [`evm-ci.yml`](./evm-ci.yml)
- [`../../Taskfile.yml`](../../Taskfile.yml)
- [`../../graft/coreth/Taskfile.yml`](../../graft/coreth/Taskfile.yml)
- [`../../graft/subnet-evm/Taskfile.yml`](../../graft/subnet-evm/Taskfile.yml)
- [`../../graft/evm/Taskfile.yml`](../../graft/evm/Taskfile.yml)
- [`../../docs/bazel.md`](../../docs/bazel.md)
