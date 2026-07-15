# Tasks

This document explains why avalanchego uses [Task](https://taskfile.dev) and
how tasks should be maintained in this repo.

For basic usage, including how to list and run tasks, see the [Running
tasks](../CONTRIBUTING.md#running-tasks) section of
[`CONTRIBUTING.md`](../CONTRIBUTING.md).

## Table of contents

- [Overview](#overview)
- [Why this repo uses Task](#why-this-repo-uses-task)
  - [Why not Make?](#why-not-make)
  - [Why Task instead of just?](#why-task-instead-of-just)
- [How tasks should work in this repo](#how-tasks-should-work-in-this-repo)
  - [Tasks used directly by CI should use stable named entrypoints](#tasks-used-directly-by-ci-should-use-stable-named-entrypoints)
  - [Exception: CI execution environment details](#exception-ci-execution-environment-details)
  - [Example: good pattern](#example-good-pattern)
  - [Example: pattern to avoid](#example-pattern-to-avoid)
- [When to add or change a task](#when-to-add-or-change-a-task)

## Overview

This repo uses Task so contributors can list useful commands and run them
consistently.

Two goals matter here:

- **Discoverability**: Invoking `task` without arguments lists available commands
- **Reproducibility**: when CI runs a task, a contributor can run the same task
  locally and expect broadly similar behavior. This is intended to maximize the
  chances of reproducing CI failures locally.

Not every task in this repo is used directly by CI. Some tasks are simple wrappers
around tools that naturally take arguments or other user input. The stronger rules
below apply mainly to tasks used directly by CI or used as stable entrypoints that
contributors are expected to rerun locally to reproduce CI behavior.

## Why this repo uses Task

Task is a command runner: a tool for defining named commands that can be
listed and run in a consistent way.

This repo wanted a command runner, not a build system. The main need was a simple way
to expose common commands for builds, tests, linting, generation, and similar work,
while keeping those commands easy to find and easy to rerun.

### Why not Make?

Make brings build dependency features and rules for deciding what needs to be
rebuilt. Those can be useful for some projects, but they were not the point
here. This repo mainly wanted stable, discoverable command names.

### Why Task instead of just?

[just](https://just.systems) could also serve as a command runner here. Task was
chosen because it fit this repo's toolchain and needs at the time:

- it was a mature enough tool for the job
- it fit naturally with the repo's existing Go-based tooling
- its structured YAML format was considered acceptable for repo maintenance

This does not mean Task needs to be a permanent fixture of the repo. What matters is
the task model, not the specific tool. The repo needs a command runner that makes
common commands easy to find and run, especially commands that CI and developers both
need to run.

## How tasks should work in this repo

Tasks should be simple named commands. In most cases, the task name should be what
people and CI use, while scripts and code hold the real behavior.

This is a general preference across the repo, and a stronger expectation for tasks
used directly by CI or reused as stable reproduction entrypoints.

That means tasks should usually:

- stay simple
- call scripts, tools, or other tasks that hold the real logic
- avoid turning into their own separate place to configure behavior

Composing a few commands or other tasks inside `Taskfile.yml` is fine when that
keeps the supported entrypoint clear. The main thing to avoid is hiding the real
configuration or behavior only in workflow callsites or Task-specific wiring that a
local user would have to rediscover.

Keeping non-trivial logic out of tasks makes it easier to apply the right linting and
validation to scripts or code. That is much harder to do when behavior only exists
inside a task definition.

### Tasks used directly by CI should use stable named entrypoints

A **CI entrypoint task** is a task CI uses as the named way to run a
repo-managed operation.

A CI entrypoint task should usually be invoked through a stable named
entrypoint.

In GitHub Actions workflows in this repo, that normally means invoking the task via
`./scripts/run_task.sh` which ensures that task will run in a variety of different
environments that may not have it directly installed.

In normal use, the caller should not need to know special:

- flags
- environment variables
- Taskfile variables
- other task-specific configuration

Tasks used directly by CI should usually not require positional arguments or other
caller-supplied input. If CI needs different behavior, that should usually be
expressed as a different named task rather than by parameterizing a single task at the
workflow callsite.

A task does not need to reject all input in every context. The potential problem is in
defining behavior via CI-only settings that a local user would have to manually apply
locally.

This repo does have some intentional exceptions. A small number of tasks are used as
stable launchers for a family of related runs where the varying input is part of the
user-facing interface rather than hidden CI-only behavior. For example, CI may select
a fuzz shard, a particular re-execution scenario, or load-test flags that a developer
can also pass locally to reproduce the same run.

As a practical guardrail, workflow task invocations should not pass extra option flags
after `--`. In this repo, `scripts/actionlint.sh` checks for that exact pattern in
workflow `run:` steps because it is usually a sign that CI is changing task behavior
where it is called instead of invoking a stable named task. This is only a narrow
guardrail, not a full semantic check of all workflow task usage.

The goal is that a contributor should usually be able to reproduce the results of a CI
command locally by running the same named task without additional configuration. Only
in exceptional cases should it be necessary to locally configure a task run by CI.

### Exception: CI execution environment details

CI sometimes needs extra settings because of the CI environment itself. That is
acceptable when those settings support how the task runs in CI, rather than changing
the fundamental behavior of the task.

For example, CI may need launcher or environment settings because of CI setup
requirements. But the same task should still be runnable locally without those CI-only
settings.

### Example: good pattern

If a workflow runs:

```bash
./scripts/run_task.sh bazel-test-main
```

then a contributor should usually be able to run the same task locally:

```bash
task bazel-test-main
```

The task may call wrappers, scripts, or tools underneath, but the caller should not
need to know those details just to rerun the command.

### Example: pattern to avoid

Avoid designs where CI is effectively defining the real command through extra
configuration that a local user has to rediscover, for example by putting the
important behavior in workflow-only task vars, flags, or environment settings.

For example, avoid the following style of job definition:

```yaml
- run: ./scripts/run_task.sh test-e2e
  env:
    E2E_FILTER: xsvm
    E2E_RUNTIME: kube
```

Reproduction of a CI failure for this job would require duplicating the environment
configured by the job rather than just invoking the task.

The problem here is not that environment variables exist at all. The problem is that
these variables change what the task does, rather than only adapting it to run inside
CI. A developer should not need those settings just to run the same task locally.

By contrast, workflows may pass arguments that are already part of the task's intended
public interface when that interface is also available to local users.  That kind of
parameterization should stay explicit and reproducible rather than being hidden in
CI-only environment configuration.

## When to add or change a task

Add or keep a task when it gives the repo a stable, discoverable command that people
or CI are expected to run.

A task is often a good fit when:

- contributors will run it regularly
- CI and local use should share the same named command
- the task makes a supported command easier to find with `task`

A task is often **not** the right fit when:

- the command is a one-off maintenance step
- the real interface needs a lot of user-supplied configuration
- the script or tool should remain the primary interface
- the task would mostly duplicate implementation detail rather than provide a useful
  stable command name

Direct script execution is acceptable when the script is the real interface and
the repo does not need a separate stable, discoverable task name for CI or
contributors. Add a task when the repo wants that named entrypoint; do not add
one when it would only hide or duplicate the real interface.

When changing an existing task, preserve the main reasons tasks exist here:

- people should be able to find supported commands easily
- tasks used directly by CI should stay easy to rerun locally
- the real behavior should stay in scripts or code, not in task-only settings
