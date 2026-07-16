# CI

This document explains the CI rules that apply across the repo.

Use it when changing workflows, adding actions, or reviewing CI changes that
should follow a shared repo rule rather than a one-off local choice.

## Table of contents

- [Overview](#overview)
- [Usage](#usage)
- [Conceptual model](#conceptual-model)
  - [Use stable repo entrypoints](#use-stable-repo-entrypoints)
  - [Jobs using `install-nix` must define shell policy](#jobs-using-install-nix-must-define-shell-policy)
  - [Composite actions must manage their own Nix shell](#composite-actions-must-manage-their-own-nix-shell)
  - [Pin runner labels](#pin-runner-labels)
  - [Pin third-party actions](#pin-third-party-actions)
  - [Only `actions/*` may use major tags](#only-actionsmay-use-major-tags)
  - [How these rules are enforced today](#how-these-rules-are-enforced-today)
- [Maintainer guidance](#maintainer-guidance)
- [References](#references)

## Overview

The main CI goals in this repo are:

- keep workflow behavior easy to find and review
- make CI runs easier to reproduce locally when that is useful
- make upgrades to runners and external actions intentional

Feature-specific CI details should live with the feature that owns them. This
page is only for rules that apply across workflows and local composite actions.

## Usage

Use this document when you are:

- adding or reviewing a GitHub Actions workflow
- adding or updating a GitHub Action from outside this repo
- deciding whether workflow logic belongs in a task, a local action, or a
  small CI-only helper
- deciding whether a workflow is doing too much work in YAML

For task-specific guidance, including when workflows should call
`./scripts/run_task.sh`, see [Tasks](./tasks.md).

## Conceptual model

### Use stable repo entrypoints

When CI runs behavior defined in this repo, it should usually call an existing
repo command, task, script, or action instead of building the real command in
the workflow.

In practice, that usually means one of:

- a named task launched with `./scripts/run_task.sh`
- a local composite action under `./.github/actions/`
- a small CI-only helper script when the behavior is truly CI glue and not a
  developer-facing entrypoint

When a workflow does call a CI-only helper script directly, the script should
follow the `scripts/workflow-*.sh` naming convention. That is the documented
exception to the normal preference for repo entrypoints such as tasks and local
actions, and `scripts/actionlint.sh` checks for it.

Keep the supported behavior visible in the repo instead of hiding it in
workflow YAML.

This helps with review and maintenance. If CI runs behavior defined in this repo, a
contributor should usually be able to find how it is launched in the tree and rerun it
or inspect it directly.

### Jobs using `install-nix` must define shell policy

**Summary:** if a job directly uses `./.github/actions/install-nix`, later `run:`
steps should default to the nix dev shell so CI uses the same flake-provided tooling
developers use locally.

Installing Nix is not the same thing as running later steps inside the nix dev
shell. A step of a nix-requiring job may succeed without a nix dev shell if the runner
environment provides the expected tools, but the version of those tools isn't
guaranteed consistent with the tools the nix shell provides. Such a divergence
increases the possibility that a CI failure won't be locally reproducible,
complicating diagnosis and resolution.

It's also possible for execution outside of a nix dev shell to increase a
given job's exposure to transitory CI failures, as shown by the motivating
failure below.

To encourage nix-requiring jobs to use the nix dev shell, the lint job runs `task
check-workflow-nix-shell` to ensure that jobs using the `install-nix` custom action
directly set `defaults.run.shell` to a shell string that starts with `nix develop`
e.g.:

```yaml
defaults:
  run:
    shell: nix develop --command bash -x {0}
```

Where required, individual steps can still set a local `shell:` declaration to
override the default. In particular, `nix develop` and `nix develop --impure` are not
interchangeable, so use per-step `shell:` declarations when a step intentionally
depends on job or GitHub Actions environment variables.

#### Motivation

The check for the use of a nix shell was added after a job that should have been using
a nix shell was observed to fail due to an infra flake:

 - `scripts/run_task.sh` was being run outside of a nix dev shell
 - `task` (intended to be provided by the nix shell) was not available
 - The script fell back to running `task` via `go run`
 - `go` (intended to be provided by the nix shell) was not the expected version
 - `go` attempted to download the configured version
 - The golang download failed due to an infra flake

Were the job in question using the nix shell, no download would have been required and
the infra flake could have been avoided.

#### Alternatives considered

Another way of solving the same problem would be to update `scripts/run_task.sh` to
automatically enter a nix shell. This would have ensured that `task` would run in a
nix dev shell, but wouldn't have guaranteed that invocations of commands other than
`task` would be guaranteed a nix shell.

### Composite actions must manage their own Nix shell

**Summary:** workflow defaults do not define the shell for a composite
action's internal `run:` steps.

If a composite action depends on flake-provided tools, the action itself should
enter the dev shell for those steps. It may rely on the caller to install Nix
first, but it should not rely on the caller's job defaults to establish the
action's own execution contract.

Examples:

- `./.github/actions/collect-kind-diagnostics` should enter `nix develop ...`
  for steps that expect flake-provided tools such as `kind` and `kubectl`
- `./.github/actions/run-monitored-tmpnet-cmd` already enters the dev shell for
  its monitored command step using `nix develop --impure ...`, so jobs that
  only rely on that action do not need a separate job-level default solely for
  that purpose

### Pin runner labels

GitHub-hosted runner labels should use explicit versions such as `ubuntu-24.04`
or `macos-26`, not floating labels such as `ubuntu-latest` or `macos-latest`.

Floating labels can change underneath the default branch without any matching
repo change. Explicit labels make runner image upgrades a reviewed repo change.

### Pin third-party actions

This repo treats actions in two groups:

- local actions in this repository
- third-party actions outside this repository

Local actions under `./.github/actions/` are part of this repo, so there is no
external version to pin.

For third-party actions, the default rule is to pin a full commit SHA. This
reduces supply-chain risk and makes upgrades explicit.

### Only `actions/*` may use major tags

This repo currently allows `actions/*` to use floating major version tags
instead of full SHAs.

This is a repo convention, not a universal rule.

The trade-off is that pinning every action by SHA would make every upgrade more
explicit, but it would also add more churn for the one provider this repo
already has to trust as part of running GitHub Actions at all.

So the current repo rule is:

- `actions/*` may use major tags such as `@v5` or `@v6`
- all other third-party actions should be pinned to full commit SHAs

The repo may still choose to pin an `actions/*` action by SHA in a specific
case. The repo rule is that everything outside `actions/*` should not float.

### How these rules are enforced today

Current automated checks include:

- workflows should usually invoke stable tasks or local actions instead of
  calling most `scripts/*` paths directly
- direct workflow script calls are allowed only for CI-only helpers following
  the `scripts/workflow-*.sh` naming convention
- workflow task invocations should not change task behavior by passing extra
  option flags after `--`
- floating GitHub-hosted runner labels are forbidden
- workflow jobs that directly use `./.github/actions/install-nix` must set
  `defaults.run.shell` to a value that starts with `nix develop`
- exact step-level `shell:` values that duplicate `defaults.run.shell` are
  flagged as redundant

That redundancy rule is intentionally narrow: only exact duplicate shell
strings should be removed. Non-exact shell values are not automatically
interchangeable, because a small shell difference can represent a real behavior
change. In particular, `nix develop ...` and `nix develop --impure ...` should
not be collapsed together.

The workflow-structure checks live in
[`scripts/actionlint.sh`](../scripts/actionlint.sh) and
[`scripts/check_workflow_nix_shell.sh`](../scripts/check_workflow_nix_shell.sh).

The Nix-shell check parses workflow YAML instead of treating it as plain text.
This repo already depends on Nix for lint tooling, so workflow checks should
prefer tools that understand YAML over ad hoc text parsing. The current check is
specifically a structural check over workflow jobs: it looks for direct
`install-nix` usage, verifies `defaults.run.shell`, and flags exact redundant
step-level `shell:` duplicates.

The action-pinning rule is checked by repo lint: `actions/*` may use major
version tags, while other third-party actions should be pinned by full SHA.
Code review still matters for deciding whether a newly introduced action is
appropriate in the first place.

## Maintainer guidance

When changing CI, work through these questions:

1. what existing repo command, task, script, or action should this use?
2. should that be a task, a local action, or a CI-only helper?
3. does this change add an external action, and if so how should it be pinned?
4. is the workflow mostly calling existing repo behavior, or is it starting to
   define the only real way to invoke a given check?

Useful review questions:

- can an engineer find the real behavior without reading too much workflow
  glue?
- if this workflow adds a third-party action outside `actions/*`, is it pinned
  by full SHA?
- does the workflow use an explicit runner image instead of a floating
  `*-latest` label?
- if a job installs Nix directly, is it explicit about which later steps run in
  the dev shell and which intentionally do not?
- if a composite action needs flake-provided tools, does the action define that
  shell behavior itself?
- if a task is involved, is CI calling a stable named task rather than shaping
  the behavior in the workflow?

If the same CI rule keeps showing up in review comments, that is usually a sign
that it should be documented here and possibly automated.

## References

- [Tasks](./tasks.md)
- [`scripts/actionlint.sh`](../scripts/actionlint.sh)
- [`scripts/check_workflow_nix_shell.sh`](../scripts/check_workflow_nix_shell.sh)
- [GitHub Actions: Reusing workflow configurations](https://docs.github.com/actions/using-workflows/avoiding-duplication)
- [GitHub Actions security hardening: pin actions to a full-length commit SHA](https://docs.github.com/actions/security-guides/security-hardening-for-github-actions#using-third-party-actions)
