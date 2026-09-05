# CI

This document explains how to maintain this repository's [GitHub
Actions](https://docs.github.com/actions) configuration. These conventions apply
to workflows and [local composite actions](https://docs.github.com/actions/sharing-automations/creating-actions/creating-a-composite-action).

## Table of contents

- [Principles](#principles)
- [How CI is organized](#how-ci-is-organized)
  - [Workflows coordinate repository operations](#workflows-coordinate-repository-operations)
  - [Keep Go CI unified](#keep-go-ci-unified)
  - [Go and Bazel CI workflow layout](#go-and-bazel-ci-workflow-layout)
  - [Go unit test platforms](#go-unit-test-platforms)
  - [Local composite actions define reusable GitHub Actions behavior](#local-composite-actions-define-reusable-github-actions-behavior)
  - [CI-only helpers implement CI-specific behavior](#ci-only-helpers-implement-ci-specific-behavior)
- [Provision CI job dependencies](#provision-ci-job-dependencies)
  - [Task](#task)
- [Using Nix in GitHub Actions](#using-nix-in-github-actions)
  - [Run `install-nix` jobs in the Nix dev shell](#run-install-nix-jobs-in-the-nix-dev-shell)
  - [Start the Nix dev shell in composite actions](#start-the-nix-dev-shell-in-composite-actions)
- [Runners and external actions](#runners-and-external-actions)
  - [Use versioned GitHub-hosted runners](#use-versioned-github-hosted-runners)
  - [Pin third-party actions](#pin-third-party-actions)
  - [Pinning does not eliminate supply-chain risk](#pinning-does-not-eliminate-supply-chain-risk)
- [Validation](#validation)

## Principles

- **Define locally runnable operations outside CI.** Put repository operations
  that contributors can run locally in tasks or scripts. Workflows call those
  entrypoints and do GitHub-specific setup. Defining locally runnable work only
  in CI slows iteration and costs more to implement and maintain.
- **Make infrastructure changes reviewable.** Use explicit runner labels and immutable
  references for third-party actions so their upgrade is visible in a repository
  change.

These are defaults, not absolute rules. Choose a different approach when it makes CI
easier to understand or maintain.

## How CI is organized

### Workflows coordinate repository operations

A workflow in [`.github/workflows/`](../.github/workflows/) defines GitHub Actions
configuration for an operation. It specifies triggers, job dependencies, permissions,
runners, containers, secrets, artifacts, and CI-only environment variables. Where
possible, workflows coordinate repository operations rather than implement them.

Run the operation through its local entrypoint. If the entrypoint is a task, use
`./scripts/run_task.sh`. See [Tasks](./tasks.md) for this repository's task
conventions.

For example, this workflow step runs the unit-test task:

```yaml
- name: Run unit tests
  run: ./scripts/run_task.sh test-unit
```

### Keep Go CI unified

Go CI checks the avalanchego, Coreth, EVM, and Subnet-EVM modules. These modules
must remain available to downstream consumers through Go tooling.

Use one task to run unit tests for all four modules. Do not add a unit-test job
for one module. Add each tested module to the Go workspace. Update package
selection in [`scripts/tests.unit.sh`](../scripts/tests.unit.sh) when necessary.

The pre-merge and scheduled entrypoints select runners and platforms. Keep
shared unit-test policy in the reusable workflows. This structure applies
changes to test selection, race detection, and test shuffling across all Go
modules.

The `Bazel` workflow checks repository code that does not need the downstream
Go-module interface. Keep Bazel checks out of the Go workflows.

Name a component-specific job `<check>-<component>`, such as `lint-evm`. Omit
the component for a repository-wide check. The aggregate job is an exception.
Include the workflow name in `go-required`. This name keeps the required check
distinct in GitHub output.

Put `go-required` first in the pre-merge workflow. Sort the other job
definitions and its `needs` list alphabetically. The `go-required` job fails if
an enabled job fails.

### Go and Bazel CI workflow layout

Go and Bazel use the same workflow roles and file-name pattern:

| Role | Bazel | Go |
| --- | --- | --- |
| Pre-merge entrypoint | [`bazel-ci-pre-merge.yml`](../.github/workflows/bazel-ci-pre-merge.yml) | [`go-ci-pre-merge.yml`](../.github/workflows/go-ci-pre-merge.yml) |
| Scheduled entrypoint | [`bazel-ci-scheduled.yml`](../.github/workflows/bazel-ci-scheduled.yml) | [`go-ci-scheduled.yml`](../.github/workflows/go-ci-scheduled.yml) |
| Primary reusable workflow | [`bazel-ci.yml`](../.github/workflows/bazel-ci.yml) | [`go-ci.yml`](../.github/workflows/go-ci.yml) |
| Reusable smoke workflow | [`bazel-ci-smoke.yml`](../.github/workflows/bazel-ci-smoke.yml) | [`go-ci-smoke.yml`](../.github/workflows/go-ci-smoke.yml) |

Entrypoints select the reusable workflow that provides the required test policy,
or define jobs that are specific to that event. The pre-merge and scheduled Go
entrypoints both use `go-ci.yml`; pre-merge macOS uses `go-ci-smoke.yml`.

Smoke workflows run a minimal macOS test. This test verifies that unit tests
can run on macOS. The Linux pre-merge job and scheduled jobs run the full unit
suite.

### Go unit test platforms

The `unit` job in `go-ci-pre-merge.yml` calls the reusable
[`go-ci.yml`](../.github/workflows/go-ci.yml) workflow on Linux AMD64. It runs
the unified unit test suite ([`scripts/tests.unit.sh`](../scripts/tests.unit.sh))
through the `test-unit` task. That task disables race detection and test
shuffling so the Go build and test cache can serve repeated runs.

On macOS, the `smoke` job calls
[`go-ci-smoke.yml`](../.github/workflows/go-ci-smoke.yml). macOS runners are
slower. They also fail more often because of external runner problems.
Pre-merge CI therefore runs only a Go unit-test smoke test on macOS. This
mirrors the macOS smoke job in Bazel CI. See [Test platforms and cache
policy](./bazel.md#test-platforms-and-cache-policy).

The `Scheduled Go` workflow runs the full unit suite on each platform. The
workflow is defined in
[`go-ci-scheduled.yml`](../.github/workflows/go-ci-scheduled.yml). It calls
[`go-ci.yml`](../.github/workflows/go-ci.yml) for each platform. Only the Ubuntu
24.04 AMD64 job runs `test-unit-race-shuffle`. This task enables race detection
and shuffled test order. The other scheduled jobs run `test-unit` to check
platform compatibility without race detection or shuffled test order.

### Local composite actions define reusable GitHub Actions behavior

Use a repository-wide local composite action under [`.github/actions/`](../.github/actions/)
when multiple jobs need the same GitHub Actions behavior. Duplicating GitHub Actions
configuration makes later changes error-prone. Put a feature-specific action with its
feature, such as
[`.github/packaging/actions/`](../.github/packaging/actions/).

A composite action can:

- set up an environment
- collect artifacts
- run a command with monitoring

For example, end-to-end jobs in
[`.github/workflows/go-ci-pre-merge.yml`](../.github/workflows/go-ci-pre-merge.yml) use
`run-monitored-tmpnet-cmd` to monitor a named task and collect its artifacts:

```yaml
- uses: ./.github/actions/run-monitored-tmpnet-cmd
  with:
    run: ./scripts/run_task.sh test-e2e-ci
```

Do not use a composite action as the only entrypoint for an operation that
must run outside CI. Keep that operation in a task or script.

### CI-only helpers implement CI-specific behavior

Use a `workflow-*.sh` helper for CI-specific behavior that only one workflow uses.
These helpers run only in CI. They usually do not need task entrypoints for local
use.

Put repository-wide CI helpers under [`scripts/`](../scripts/), such as
[`scripts/workflow-build-tgz-pkg.sh`](../scripts/workflow-build-tgz-pkg.sh). Put
feature-specific helpers with the feature, such as
[`.github/packaging/scripts/workflow-setup-packaging.sh`](../.github/packaging/scripts/workflow-setup-packaging.sh).

`scripts/actionlint.sh` allows workflow calls to helpers named `workflow-*.sh`. Do
not use that allowance for an operation that should be a task or normal script.

## Provision CI job dependencies

Nix provides the repository's preferred local development environment. See
[Using the dev shell](../CONTRIBUTING.md#using-the-dev-shell). GitHub-hosted
runners are ephemeral, so installing Nix adds setup work to every job that uses
it.

Because installing Nix is per-job work on hosted runners, `install-nix` is
reserved for jobs with dependencies that another setup action does not provide.

| Dependency | Provisioning mechanism | Use when |
| --- | --- | --- |
| Go | [`setup-go-for-project`](../.github/actions/setup-go-for-project/) | The job needs Go and does not use Nix or Bazel to provide it. |
| Bazel | [`setup-bazel`](../.github/actions/setup-bazel/) | The job needs Bazel, which also provides Go. |
| Flake-provided tools | [`install-nix`](../.github/actions/install-nix/) | A job runs a command that requires a dependency supplied by the Nix dev shell, which also provides Go. |

`setup-go-for-project`, `setup-bazel`, and `install-nix` are alternative Go
provisioning mechanisms. A job that uses `setup-bazel` can also use `install-nix`
for dependencies that Bazel does not provide.

### Task

[Task](https://taskfile.dev) runs repository operations in CI. The local
[`setup-task`](../.github/actions/setup-task/action.yml) action makes the Task
binary available to Go, Bazel, and Docker jobs. Run this action after checkout
because it reads `tools/external/go.mod`. Run it after the job reclaims disk
space. Disk cleanup removes the runner tool cache, including Go.

See [Task version](./tasks.md#task-version) for the version policy and update
commands.

The action uses a GitHub Actions cache, not an artifact. A cache lets unrelated
jobs and workflow runs reuse one binary. An artifact belongs to one workflow
run. The cache key includes the Task version, operating system, and
architecture. Each job restores the matching cache. Only a `push` to `master`
saves a cache entry. Pull request and merge-queue jobs download Task on a cache
miss. They do not save the binary. This policy limits cache storage to merged
versions and prevents unmerged code from producing shared cache contents.

A cache entry exists only after a `master` job runs on the same operating system
and architecture. When you add a CI platform, add a `master` job for that
platform if it needs a reusable Task cache. Otherwise, cache-miss jobs download
Task.

On a cache miss, the action downloads the platform release from the Task GitHub
release and checks its SHA-256 value against that release's checksum file. This
avoids a host Go dependency in Bazel jobs. A source build would require Go or a
Bazel Task bootstrap target before Task can run. The checksum detects a damaged
or changed archive in transit. It does not independently prove the release
source because the action downloads both files from the same release.

When changing this setup, keep these rules:

- Keep Nix and `tools/external/go.mod` on the same Task version.
- Keep cache writes limited to pushes to `master`.
- Keep the release platform mapping compatible with every CI runner.
- Keep Task setup after disk cleanup in jobs that use disk cleanup.

## Using Nix in GitHub Actions

Installing Nix makes Nix available, but does not ensure later commands use the
Nix development shell.

### Run `install-nix` jobs in the Nix dev shell

A job that directly uses `./.github/actions/install-nix` must set its default shell to
`nix develop`.

CI previously failed when a job installed Nix but ran `scripts/run_task.sh` from a
step outside the dev shell. In these jobs, the dev shell, rather than
`setup-go-for-project`, supplies Task and the required Go version.

The failure occurred as follows:

1. `task` was not in the `PATH`.
2. `scripts/run_task.sh` ran `task` with `go run`.
3. The runner Go version was in the `PATH`.
4. The runner Go version differed from the repository version.
5. Go downloaded the required version.
6. The download failed and failed the job.

Using the Nix dev shell avoids this failure mode by ensuring that `task` and the
required Go version are in the `PATH`.

An alternative to setting the Nix dev shell in the workflow could be to start it in
`scripts/run_task.sh`. This would protect task calls, but not direct script calls. A
job default shell protects both.

Set the job default shell as follows:

```yaml
defaults:
  run:
    shell: nix develop --command bash -x {0}
```

Set a step shell only when that step needs different behavior from the default. For
example, a step that reads GitHub Actions environment variables can use `nix develop
--impure --command bash -x {0}`.

### Start the Nix dev shell in composite actions

A composite action cannot set `defaults.run.shell`. A calling job's default shell does
not apply to the action. Set `shell:` on each `run:` step that needs the Nix dev
shell.

Composite actions that use the Nix dev shell currently expect the calling job to
install Nix. Future work could remove this requirement by making the `install-nix`
composite action idempotent so that jobs and custom actions could safely invoke it
repeatedly.

## Runners and external actions

### Use versioned GitHub-hosted runners

Use an explicit GitHub-hosted runner label, such as `ubuntu-24.04` or `macos-26`,
rather than `ubuntu-latest` or `macos-latest`. A floating label can move to a new OS
version without a reviewed repository change.

Versioned labels do not make runner images immutable. GitHub can update the image for
a versioned label without a repository change, and those updates can break CI. A
versioned label prevents an unreviewed move to a new OS version.

### Pin third-party actions

This repository uses three types of actions:

- Local actions are part of this repository. They have no external reference to
  pin. Examples include [`.github/actions/`](../.github/actions/) and
  [`.github/packaging/actions/`](../.github/packaging/actions/).
- This repository treats GitHub-maintained [`actions/*`](https://github.com/actions)
  as part of the GitHub Actions platform. They may use a moving major-version tag,
  such as `actions/checkout@v5`. This ensures the repository receives compatible
  platform updates automatically.
- Other action publishers are not trusted to use floating tags. Pin their actions
  to a full commit SHA. This ensures that every update to the pinned action reference
  is subject to review. The SHA identifies the code that reviewers approved.

For example:

```yaml
- uses: docker/setup-qemu-action@ce360397dd3f832beb865e1373c09c0e9f86d70a # v4
```

A full [commit SHA](https://docs.github.com/en/actions/reference/security/secure-use#using-third-party-actions)
is immutable. A tag can move.

Add a `# <tag>` comment after every pinned SHA. The comment identifies the tag
that this repository intends to track for readers and
[Dependabot](https://docs.github.com/code-security/dependabot). This repository configures
Dependabot to open pull requests only for security updates. A working action does not need
routine tag updates. Routine updates can include JavaScript dependency changes that would
be challenging to qualify.

Review each security update as a third-party action upgrade. Review the pinned
code, its permissions, and the workflow change.

### Pinning does not eliminate supply-chain risk

A full SHA pins only the action that this repository references. That action can run
arbitrary code, invoke another action by a mutable tag, or download an unpinned
dependency. Pinning reduces one source of change. It does not make an action or its
dependency chain safe.

When adding or upgrading a third-party action, review its source and its
dependencies. Prefer actions that pin the third-party actions they invoke. Consider
the action's permissions and the job's sensitivity when deciding how much review is
needed.

## Validation

After changing GitHub Actions configuration, run `task lint-action`. The `lint-all`
and `lint-all-ci` tasks also run `lint-action`. In addition to `actionlint`,
[`scripts/actionlint.sh`](../scripts/actionlint.sh) checks:

- direct calls from workflows to `scripts/`, except `run_task.sh` and `workflow-*.sh`
  helpers
- task calls from workflows that pass option flags after `--`
- third-party action references without full SHAs and tag comments
- floating `ubuntu-latest` and `macos-latest` runner labels
- jobs that use `install-nix` without a `nix develop` default shell
- step shells that duplicate the job default shell

[`scripts/check_workflow_nix_shell.sh`](../scripts/check_workflow_nix_shell.sh)
checks the last two rules from the workflow YAML. It only rejects an exact
duplicate because `nix develop` and `nix develop --impure` behave differently.

These checks catch common violations, but they do not prove that a workflow is
correct. Always review the workflow's permissions, inputs, secrets, failure handling,
and exceptions to these conventions.
