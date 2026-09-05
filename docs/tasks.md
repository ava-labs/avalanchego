# Tasks

This document explains why avalanchego uses [Task](https://taskfile.dev) and
how to maintain tasks in this repo.

For basic usage, including how to list and run tasks, see [Running
tasks](../CONTRIBUTING.md#running-tasks) in
[`CONTRIBUTING.md`](../CONTRIBUTING.md).

## Table of contents

- [Principles](#principles)
- [Why this repo uses Task](#why-this-repo-uses-task)
  - [The double dash delimiter](#the-double-dash-delimiter)
  - [Why not Make or just?](#why-not-make-or-just)
- [How tasks should work in this repo](#how-tasks-should-work-in-this-repo)
  - [Keep tasks simple](#keep-tasks-simple)
  - [Keep Taskfile entries sorted](#keep-taskfile-entries-sorted)
  - [CI should run named tasks](#ci-should-run-named-tasks)
  - [Some CI-only setup still belongs in workflows](#some-ci-only-setup-still-belongs-in-workflows)
- [Examples](#examples)
  - [Good: CI runs a named task](#good-ci-runs-a-named-task)
  - [Good: the workflow passes a normal task argument](#good-the-workflow-passes-a-normal-task-argument)
  - [Avoid: the workflow changes what the task does](#avoid-the-workflow-changes-what-the-task-does)
- [When to add or change a task](#when-to-add-or-change-a-task)

## Principles

- Defining common repo operations with Task makes them **easy to find**.
  - `task` without arguments lists supported commands.
- Requiring that CI runs named tasks makes **CI easier to reproduce locally**.
  - A contributor can usually rerun the same task locally.
  - Local execution may still depend on supporting tools such as nix or bazel.

## Why this repo uses Task

Task is a tool for naming and running commands. This repo wanted that kind of tool,
not a build system.

What matters is less the specific tool than what it enables. The repo needs a way to
define stable, discoverable command names for builds, tests, linting, code generation,
and similar work. Task was a reasonable fit for that need.

### Why not Make or just?

Make is most useful when a repo needs build rules and dependency tracking, but this
repo already has solutions for those concerns. This repo just needs a way to define
and discover commands on top of existing tooling. Make is more tool than is needed for
that job, and its configuration format can be harder for casual users to understand.

[just](https://just.systems) is a reasonable alternative to Task. Its syntax is more
like Make or shell scripting, which some people may prefer to Task's YAML style. Task
was chosen over just because the existing dependency on Go tooling made adoption
simpler and the repo's use of GitHub Actions also meant that YAML was already a
familiar format.

### The double dash delimiter

Task has one quirk worth noting: task arguments come after `--` (for example,
`task task-name -- arg1 arg2`). Without that delimiter, Task treats additional
words as more task names rather than arguments.

This repo generally prefers tasks that can be run without extra arguments, so
that tradeoff was considered acceptable.

## How tasks should work in this repo

These guidelines describe the repo's default approach to tasks, not rules that
forbid every exception. When a different design is warranted, it should still
be easy to explain in terms of making commands easy to find and easy to rerun
locally.

### Keep tasks simple

In most cases, the task name should be the thing people run, while scripts and code do
the real work.

Task names should be clear enough that someone scanning `task` output can identify the
command they want. For common repo operations, the task name should ideally also be a
standard, memorable entrypoint that people can reuse in discussion, review, CI, and
local reproduction. Names do not need to carry every detail because the `desc` field of
a task can provide additional context, but they should avoid unusual wording that makes
a task's purpose harder to recognize.

In practice, tasks should usually:

- stay simple
- call scripts, tools, or other tasks
- avoid requiring extra workflow-only configuration

Most non-trivial shell logic should usually live under [`scripts/`](../scripts/). The
main exception is code under [`.github/`](../.github/) when it is specific to GitHub
Actions or packaging.

This makes the real behavior easier to check, test, reuse, and review.

### Keep Taskfile entries sorted

List public tasks in alphabetical order by name. Keep `default` first. You can group
internal tasks, whose names start with `_`, near the tasks that use them.

Sorting makes `task --list` and Taskfile review easier. Keep the order when you
add, rename, or remove a task.

### CI should run named tasks

When CI runs a repo operation, it should usually do so by running a named task. CI
behavior existing only in GitHub Actions YAML is harder to discover, run, and
maintain.

In this repo, that usually means calling `./scripts/run_task.sh` from a workflow. That
helps keep the task runnable even when `task` is not already installed.

For tasks that CI runs directly, the caller should usually not need special:

- flags
- environment variables
- Taskfile variables
- other task-only configuration

If CI needs different behavior, prefer a different named task instead of having the
workflow reconfigure a shared task. Avoid workflow-only configuration that forces
contributors to reconstruct the CI invocation manually when reproducing a failure
locally.

An exception is tasks that already support many different configurations. When a
separate task for each configuration would be too cumbersome, CI may pass arguments to
choose one, as long as contributors can use the same form locally. See [Good: the
workflow passes a normal task argument](#good-the-workflow-passes-a-normal-task-argument).

As a practical, best-effort check, workflow task invocations should usually not pass
extra option flags after `--`. In this repo, `scripts/actionlint.sh` checks for that
pattern in workflow `run:` steps. The check is best-effort: it only catches a common
mistake rather than every possible form.

### Some CI-only setup still belongs in workflows

CI sometimes needs extra CI-specific setup. That is usually fine when the setup is
about the environment rather than about changing what repo operation is being run.

For example, workflows may still need runner choice, container setup, artifact
uploads, or secrets. That is normal CI setup, not task definition.

## Examples

### Good: CI runs a named task

In [`.github/workflows/go-ci-pre-merge.yml`](../.github/workflows/go-ci-pre-merge.yml), the process-based
load test runs:

```bash
./scripts/run_task.sh test-load-ci
```

A contributor can run the same thing locally with:

```bash
task test-load-ci
```

The task may call other tasks, scripts, or tools underneath, but the workflow
is not hiding extra behavior.

### Good: the workflow passes a normal task argument

In [`.github/workflows/fuzz.yml`](../.github/workflows/fuzz.yml), CI runs:

```bash
./scripts/run_task.sh test-fuzz-long -- ./vms/platformvm
```

That is fine because the path is part of the task's normal interface, and a
contributor can run the same form locally:

```bash
task test-fuzz-long -- ./vms/platformvm
```

The workflow is choosing from an existing task interface rather than inventing
hidden CI-only behavior.

### Avoid: the workflow changes what the task does

Prefer to avoid designs where the workflow defines the real command through
workflow-only environment variables or task configuration.

For example:

```yaml
- run: ./scripts/run_task.sh test-e2e
  env:
    E2E_FILTER: xsvm
    E2E_RUNTIME: kube
```

That forces a contributor to reconstruct workflow state instead of rerunning a clear
named task. In a case like this, prefer a dedicated task such as `test-e2e-kube-ci`.

## When to add or change a task

Add or keep a task when it gives the repo a stable, discoverable command that people
or CI are expected to run.

A task is often a good fit when:

- contributors will run it regularly
- CI and local use should share the same command name
- the task makes a supported command easier to find with `task`

A task is often **not** the right fit when:

- the command is a one-off maintenance step
- the real command needs a lot of user-supplied configuration
- the script or tool should remain the main thing people run
- the task would mostly duplicate lower-level details instead of adding a useful
  command name

Direct script execution is fine when the script itself is the main thing people should
run, or when the command is too one-off to benefit from a stable task name. Add a task
only when a given capability would benefit from being discoverable and reproducible.

When changing an existing task, check whether the change still leaves the task easy to
find in `task` output, easy to rerun locally if CI uses it, and free of behavior that
is only defined in workflow YAML.
