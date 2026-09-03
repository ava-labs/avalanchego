# CI

This document explains how to maintain this repository's [GitHub
Actions](https://docs.github.com/actions) configuration. These conventions apply
to workflows and [local composite actions](https://docs.github.com/actions/sharing-automations/creating-actions/creating-a-composite-action).

## Table of contents

- [Principles](#principles)
- [How CI is organized](#how-ci-is-organized)
  - [Workflows coordinate repository operations](#workflows-coordinate-repository-operations)
  - [Keep Go CI in one workflow](#keep-go-ci-in-one-workflow)
  - [Local composite actions define reusable GitHub Actions behavior](#local-composite-actions-define-reusable-github-actions-behavior)
  - [CI-only helpers implement CI-specific behavior](#ci-only-helpers-implement-ci-specific-behavior)
- [Provision CI job dependencies](#provision-ci-job-dependencies)
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

### Keep Go CI in one workflow

The `Go` workflow, defined in [`ci.yml`](../.github/workflows/ci.yml), checks
the Go modules for avalanchego, Coreth, EVM, and Subnet-EVM. These modules must
remain available to downstream consumers through Go tooling. Do not add a
separate pre-merge Go workflow for one of these modules. Add its job to
`ci.yml`.

The `Bazel` workflow checks repository code that does not need this downstream
Go-module interface. Keep Bazel checks out of `ci.yml`.

This split keeps the Go-module test policy in one place. A change to runners,
caches, test selection, race detection, or test shuffling can then apply to
every Go module. The workflow has one required job. It checks every enabled
job.

Name a job `<check>-<component>`, such as `unit-avalanchego` or `lint-evm`.
For a repository-wide check, omit the component. The aggregate job is an
exception. Include the workflow name: `go-required`. This keeps the required
check distinct in GitHub output. Matrix job names include `${{ matrix.os }}` so
each platform check has a distinct name. Put `go-required` first. Sort all
other job definitions and its `needs` list alphabetically.

All jobs run for every event that starts the workflow. The `go-required` job
fails if any job fails.

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
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) use
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

## Using Nix in GitHub Actions

Installing Nix makes Nix available, but does not ensure later commands use the
Nix development shell.

### Run `install-nix` jobs in the Nix dev shell

A job that directly uses `./.github/actions/install-nix` must set its default shell to
`nix develop`.

CI previously failed when a job installed Nix but ran `scripts/run_task.sh` from a
step outside the dev shell. In these jobs, the dev shell, rather than
`setup-go-for-project`, supplies `task` and the required Go version.

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
