# Test workflows

## Table of contents

- [Overview](#overview)
- [Usage](#usage)
  - [Default PR coverage](#default-pr-coverage)
  - [`ci/pre-merge`](#cipre-merge)
  - [`ci/all-platforms`](#ciall-platforms)
  - [Scheduled and manual runs](#scheduled-and-manual-runs)
- [Design](#design)
  - [Bazel is the PR authority](#bazel-is-the-pr-authority)
  - [Pre-merge tests protect developers](#pre-merge-tests-protect-developers)
  - [Scheduled tests protect releases](#scheduled-tests-protect-releases)
  - [Why Bazel jobs use one workflow](#why-bazel-jobs-use-one-workflow)
- [Maintainer guidance](#maintainer-guidance)
  - [Test modes and caching](#test-modes-and-caching)
  - [Required-job behavior](#required-job-behavior)
  - [Run the same tasks locally](#run-the-same-tasks-locally)
  - [When to change this policy](#when-to-change-this-policy)

## Overview

This document describes unit-test and E2E-test coverage for pull requests,
merge-group runs, scheduled runs, and manual runs.

- Default PRs use Ubuntu 24.04 amd64.
- Pre-merge runs use Ubuntu 24.04 amd64 and macOS 26 arm64.
- Scheduled runs use Ubuntu 22.04 amd64 and arm64, Ubuntu 24.04 amd64 and
  arm64, and macOS 26 arm64.

Bazel is the unit-test authority for default PRs. Default PRs use native Go
only for tests that Bazel does not run. This avoids duplicate test paths.
Pre-merge runs add native Go and macOS coverage. Scheduled runs add the other
platforms and the `race-shuffle` mode. Contributors can request either larger
set with a PR label.

## Usage

### Default PR coverage

A default PR runs Bazel unit tests and Bazel E2E tests on Ubuntu 24.04 amd64.
Bazel unit tests use the default mode. Race detection and shuffle are off.

The native `Unit` and E2E jobs in [`ci.yml`](./ci.yml) do not run. The
Coreth, Subnet-EVM, and EVM Go unit-test jobs also do not run. Coreth Warp and
Subnet-EVM Warp and load E2E jobs run on Ubuntu 24.04 amd64. Bazel does not run
these tests.

### `ci/pre-merge`

Add the `ci/pre-merge` label to a PR. The label runs the same test coverage as
a merge-group run.

The label runs Bazel and native Go unit and E2E tests on Ubuntu 24.04 amd64 and
macOS 26 arm64. It enables the native `Unit` job and the Coreth, Subnet-EVM,
and EVM Go unit-test jobs. It also runs Coreth Warp and Subnet-EVM Warp and
load E2E tests. Unit tests use the default mode.

The Bazel E2E task builds AvalancheGo with race detection. The E2E test suite
does not use race detection.

### `ci/all-platforms`

Add the `ci/all-platforms` label to a PR. The label starts
[`unit-tests-scheduled.yml`](./unit-tests-scheduled.yml). Use it to check a
change to scheduled coverage or to reproduce a scheduled failure.

The workflow runs Bazel and native Go unit tests on all five scheduled
platforms. It uses the `race-shuffle` mode. It runs Bazel E2E tests once on each
platform. The Bazel E2E task builds AvalancheGo with race detection. The test
suite does not use race detection.

### Scheduled and manual runs

Scheduled runs execute the same root coverage as the `ci/all-platforms` label.
They run Bazel and native Go unit tests with `race-shuffle`. They run Bazel E2E
tests once on each scheduled platform.

The Coreth, Subnet-EVM, and EVM workflows also run every day. Their unit tests
run on all five scheduled platforms with `race-shuffle`. Coreth Warp and
Subnet-EVM Warp and load E2E tests run on the same platforms.

You can start these workflows with `workflow_dispatch`. Manual root runs use
the scheduled root coverage. Manual Coreth and Subnet-EVM runs include the
scheduled E2E coverage. Manual graft workflows skip lint jobs.

## Design

### Bazel is the PR authority

Default PRs do not require native Go unit tests. Bazel covers the same unit
tests. This reduces PR runtime without removing the Bazel test path.

### Pre-merge tests protect developers

Pre-merge runs add native Go coverage and macOS coverage. They test the paths
that developers use without Bazel. They run before merge because a failure can
disrupt developers who use `master`.

### Scheduled tests protect releases

Scheduled runs add Ubuntu 22.04 and Linux arm64 coverage. They also use
`race-shuffle`. These tests can find platform-specific and timing-dependent
failures before a release. They do not delay ordinary PR iteration.

### Why Bazel jobs use one workflow

Bazel jobs share a setup job. The setup job prepares dependencies and cache
state. GitHub Actions only permits `needs` dependencies in one workflow. The
Bazel jobs therefore remain in one workflow. The workflow has one required
summary job.

## Maintainer guidance

### Test modes and caching

The workflows use four unit-test modes:

- **default**: race detection off and shuffle off; used for default PRs and
  pre-merge runs
- **race**: race detection on and shuffle off; available for local validation
- **shuffle**: race detection off and shuffle on; available for local validation
- **race-shuffle**: race detection on and shuffle on; used for scheduled runs

Bazel can reuse default-mode test results. The `race`, `shuffle`, and
`race-shuffle` Bazel configurations disable test-result caching. Native Go
unit-test jobs write coverage profiles. Go does not cache a test command that
writes a coverage profile. Race detection and shuffle also prevent Go from
caching test results.

### Required-job behavior

Required summary jobs accept `skipped` only for jobs that the workflow can skip.

When you change a job condition in [`bazel-ci.yml`](./bazel-ci.yml),
[`ci.yml`](./ci.yml), [`coreth-ci.yml`](./coreth-ci.yml),
[`subnet-evm-ci.yml`](./subnet-evm-ci.yml), or [`evm-ci.yml`](./evm-ci.yml),
update its required-summary logic in the same change.

[`unit-tests-scheduled.yml`](./unit-tests-scheduled.yml) is not a required PR
check. The `ci/all-platforms` label starts it, but its result is not required,
so the workflow has no required summary job.

### Run the same tasks locally

Workflows call public `task` names that also work locally.

Bazel unit-test workflows use scoped task names. Examples include
`bazel-test-unit-avalanchego`, `bazel-test-unit-avalanchego-race`, and
`bazel-test-unit-avalanchego-race-shuffle`.

Native Go unit-test workflows use `test-unit`, `test-unit-race`,
`test-unit-shuffle`, and `test-unit-race-shuffle`. Graft workflows use
`build-test`, `build-test-race`, `build-test-shuffle`, and
`build-test-race-shuffle`.

### When to change this policy

Change this policy when one or more conditions apply:

- PR duration no longer requires reduced unit-test coverage.
- Scheduled `race-shuffle` runs do not find enough failures to justify their
  runtime.
- Scheduled-only failures occur often enough that the `ci/all-platforms` label
  does not provide enough feedback.
- Bazel and native Go test paths no longer cover different behavior.
- Scheduled runs need Bazel coverage beyond unit tests and E2E tests.
