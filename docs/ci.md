# CI

This document explains how to maintain this repository's [GitHub
Actions](https://docs.github.com/actions) configuration. These conventions apply
to workflows and [local composite actions](https://docs.github.com/actions/sharing-automations/creating-actions/creating-a-composite-action).

## Table of contents

- [Principles](#principles)
- [How CI is organized](#how-ci-is-organized)
  - [Workflows coordinate repository operations](#workflows-coordinate-repository-operations)
  - [Keep Go CI unified](#keep-go-ci-unified)
  - [Required checks and the merge queue](#required-checks-and-the-merge-queue)
  - [Go and Bazel CI workflow layout](#go-and-bazel-ci-workflow-layout)
  - [Platform-specific setup dependencies](#platform-specific-setup-dependencies)
  - [Go unit test platforms](#go-unit-test-platforms)
  - [Local composite actions define reusable GitHub Actions behavior](#local-composite-actions-define-reusable-github-actions-behavior)
  - [CI-only helpers implement CI-specific behavior](#ci-only-helpers-implement-ci-specific-behavior)
  - [C-Chain reexecution benchmarks](#c-chain-reexecution-benchmarks)
- [Provision CI job dependencies](#provision-ci-job-dependencies)
- [CI cache policy](#ci-cache-policy)
  - [Cache policy overview](#cache-policy-overview)
  - [Input-cache lifecycle](#input-cache-lifecycle)
    - [Event behavior](#event-behavior)
    - [Go module cache](#go-module-cache)
    - [Bazel dependency cache](#bazel-dependency-cache)
    - [Nix store cache](#nix-store-cache)
    - [Changing input caches safely](#changing-input-caches-safely)
  - [Task](#task)
- [Using Nix in GitHub Actions](#using-nix-in-github-actions)
  - [Run `install-nix` jobs in the Nix dev shell](#run-install-nix-jobs-in-the-nix-dev-shell)
  - [Start the Nix dev shell in composite actions](#start-the-nix-dev-shell-in-composite-actions)
- [Runners and external actions](#runners-and-external-actions)
  - [Use versioned GitHub-hosted runners](#use-versioned-github-hosted-runners)
  - [Pin third-party actions](#pin-third-party-actions)
  - [Pinning does not eliminate supply-chain risk](#pinning-does-not-eliminate-supply-chain-risk)
- [Validation](#validation)
  - [Required-check validation in GitHub](#required-check-validation-in-github)

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

### Required checks and the merge queue

Go and Bazel separate queue admission from permission to merge. GitHub accepts
skipped required jobs for queue admission. Maintainers can therefore enqueue a
pull request before its tests finish, even if those tests fail. This avoids
waiting for pull-request CI when a maintainer expects merge-group CI to pass.
A failing pull request can waste queue work. Merge-group checks must still pass
before GitHub merges the pull request.

Branch protection or a ruleset must require the merge queue for each target
branch. Required status checks alone do not enforce this policy because GitHub
accepts skipped checks. Without mandatory queue use, a pull request can merge
directly without passing merge-group tests. Do not allow direct merges that
bypass the queue.

The merge-group Go entrypoint defines `go-required` first. It depends on the
reusable test-workflow call and fails if that call fails. Each merge-group Bazel
aggregate depends on its platform's reusable test-workflow call.

A job with `needs` waits for its dependencies before GitHub skips it.
Pull-request entrypoints therefore define the same aggregate checks without
`needs` and skip them immediately. The test workflows still run. Keep the
entrypoints separate so queue admission does not wait for their tests.

Required status checks use the check name, optionally restricted to a source
app, rather than the workflow display name. Preserve these exact names in both
entrypoints:

- `go-required`
- `linux-amd64 / bazel-required`
- `darwin-arm64 / bazel-required`

Update branch protection before changing a required check name. A different
workflow display name does not isolate duplicate check names. See GitHub's
[required status check guidance](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches#require-status-checks-before-merging).

Keep the `Go` and `Bazel` workflow display names consistent between their
entrypoints. These names also form part of the concurrency groups; they do not
identify required checks.

Keep the entrypoint triggers disjoint:

- Pull-request entrypoints run on `pull_request`.
- Merge-group entrypoints run on `merge_group`, pushes to `master` and `dev`,
  and tag pushes.

Do not let both entrypoints emit the same check for one commit. Duplicate check
names can make the required result ambiguous.

### Go and Bazel CI workflow layout

Go and Bazel use the same workflow roles and file-name pattern:

| Role | Bazel | Go |
| --- | --- | --- |
| Pull-request entrypoint | [`bazel-pull-request.yml`](../.github/workflows/bazel-pull-request.yml) | [`go-pull-request.yml`](../.github/workflows/go-pull-request.yml) |
| Merge-group entrypoint | [`bazel-merge-group.yml`](../.github/workflows/bazel-merge-group.yml) | [`go-merge-group.yml`](../.github/workflows/go-merge-group.yml) |
| Scheduled entrypoint | [`bazel-scheduled.yml`](../.github/workflows/bazel-scheduled.yml) | [`go-scheduled.yml`](../.github/workflows/go-scheduled.yml) |
| Primary reusable workflow | [`bazel.yml`](../.github/workflows/bazel.yml) | [`go-unit.yml`](../.github/workflows/go-unit.yml) |
| Pull-request and merge-group reusable workflow | n/a | [`go.yml`](../.github/workflows/go.yml) |
| Reusable smoke workflow | [`bazel-smoke.yml`](../.github/workflows/bazel-smoke.yml) | [`go-smoke.yml`](../.github/workflows/go-smoke.yml) |

Entrypoints select the reusable workflow that provides the required test policy,
or define jobs that are specific to that event. The pull-request and
merge-group Go entrypoints use `go.yml`; scheduled Go uses `go-unit.yml`.

Smoke workflows run a minimal macOS test. This test verifies that unit tests
can run on macOS. The Linux pre-merge job and scheduled jobs run the full unit
suite.

### Platform-specific setup dependencies

The Go workflows define named platform setup jobs. A Linux job needs only the
Linux setup job. A macOS job needs only the macOS setup job. Do not replace these
jobs with one matrix job unless every consumer can wait for every matrix
entry. GitHub Actions lets `needs` name the matrix job, but not one matrix entry.

Define platform setup jobs after required jobs and before jobs that depend on
them. This keeps the workflow dependency graph readable.

Bazel avoids this cross-platform dependency problem. Each platform calls a
reusable workflow separately, and each call contains its own setup and consumer
jobs. Therefore, a platform's Bazel jobs wait only for that platform's setup job.

### Go unit test platforms

The `unit` job in [`go.yml`](../.github/workflows/go.yml) calls the reusable
[`go-unit.yml`](../.github/workflows/go-unit.yml) workflow on Linux AMD64. It
runs
the unified unit test suite ([`scripts/tests.unit.sh`](../scripts/tests.unit.sh))
through the `test-unit` task. That task disables race detection and test
shuffling so the Go build and test cache can serve repeated runs.

On macOS, the `smoke` job calls
[`go-smoke.yml`](../.github/workflows/go-smoke.yml). macOS runners are
slower. They also fail more often because of external runner problems.
Pre-merge CI therefore runs only a Go unit-test smoke test on macOS. This
mirrors the macOS smoke job in Bazel CI. See [Test platforms and cache
policy](./bazel.md#test-platforms-and-cache-policy).

The `Scheduled Go` workflow runs the full unit suite on each platform. The
workflow is defined in
[`go-scheduled.yml`](../.github/workflows/go-scheduled.yml). It calls
[`go-unit.yml`](../.github/workflows/go-unit.yml) for each platform. Only the Ubuntu
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

Check out the repository before a workflow uses a local action. GitHub must read
its `action.yml` from the workspace before it can run the action. A local action
cannot check out the repository for its own first use.

For example, end-to-end jobs in
[`.github/workflows/go.yml`](../.github/workflows/go.yml) use
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

### C-Chain reexecution benchmarks

The C-Chain reexecution benchmark workflows call the
[`c-chain-reexecution-benchmark`](../.github/actions/c-chain-reexecution-benchmark/action.yml)
action. The action owns the benchmark setup: it invokes
[`install-nix`](../.github/actions/install-nix/action.yml) and, when its
`firewood-ref` or `libevm-ref` input is set, runs `run-polyrepo` before the
benchmark. Firewood triggers benchmark requests through the GitHub API. The
pull-request trigger verifies that this machinery remains usable in avalanchego
CI. Keep workflow-specific triggers, matrices, and runner setup in the workflows.
Do not duplicate dependency provisioning or `run-polyrepo` there.

## Provision CI job dependencies

Nix provides the repository's preferred local development environment. See
[Using the dev shell](../CONTRIBUTING.md#using-the-dev-shell). GitHub-hosted
runners are ephemeral, so installing Nix adds setup work to every job that uses
it.

Because installing Nix is per-job work on hosted runners, `install-nix` is
reserved for jobs with dependencies that another setup action does not provide.

| Dependency | Provisioning mechanism | Use when |
| --- | --- | --- |
| Go | [`setup-go-for-project`](../.github/actions/setup-go-for-project/) | The job needs Go and Task and does not use Nix or Bazel to provide them. |
| Bazel | [`setup-bazel`](../.github/actions/setup-bazel/) | The job needs Bazel and Task. Bazel also provides Go. |
| Flake-provided tools | [`install-nix`](../.github/actions/install-nix/) | A job runs a command that requires a dependency from the Nix dev shell. The shell provides Go and Task. |

`setup-go-for-project` and `setup-bazel` install the pinned Task release.
`install-nix` makes the Nix dev-shell Task available. See [Task](#task) for the
cache and version rules.

Choose the setup action that provides the job's dependencies. Bazel provides
Go for its targets, but a job that also runs host Go commands needs
`setup-go-for-project` or the Nix dev shell. For example, `check-go-mod-tidy`
uses both `setup-bazel` and `setup-go-for-project`, while `lint-avalanchego`
uses `setup-bazel` and `install-nix` for flake-provided tools.

When combining these actions, run `setup-bazel` before either Go setup action
disables `GOPROXY`. Bazel preparation can download Go modules into its separate
module cache on a miss or non-exact restore. Preparing the host Go module cache
does not populate that cache. An exact Bazel cache hit can hide an incorrect
setup order, so validate both exact hits and cold-cache preparation; see
[Bazel cache validation](./bazel.md#changing-this-safely).

`install-nix` can restore Go module input, but it does not save that cache. The
Go workflow setup jobs own Go module, Task, and Nix store cache writes. Other
workflows consume these caches without writing them.

## CI cache policy

### Cache policy overview

CI caches tools and dependencies, not build or test output. Two rules govern
these input caches:

- **Save shared entries only from designated setup jobs on `master`.** Pull
  requests and other refs may restore entries and prepare missing inputs locally,
  but cannot save them. This reserves limited cache storage for merged code.
- **Verify Go and Bazel dependency inputs before using them.** On an exact cache
  hit, use the restored entry. On a non-exact hit or miss, prepare the inputs
  locally (and save them if permitted). Then disable downloads for the workload:
  `GOPROXY=off` for Go modules and for Gazelle repository rules, and
  `--repository_disable_download` for Bazel repository downloads. If a
  dependency is missing, the workload fails instead of fetching it silently.

The second rule checks whether cache preparation covers what CI actually uses;
restricting writes alone cannot do that. It catches missing preparation inputs
when a pull request adds a dependency, as well as on `master`. Disabling
downloads detects gaps in cache preparation as CI changes; it is not a
prerequisite for preparing the cache, which can use the network. The Task cache
can download a release on a miss, and the Nix store cache can fetch missing
flake store paths. Those caches do not use the offline completeness check.

### Input-cache lifecycle

GitHub-hosted runners are temporary. GitHub Actions caching is currently used
for build inputs - tools and dependencies - not outputs (build and testing). A
cache miss must still let the job obtain its required input before the job runs
offline. GitHub Actions caches are immutable: the first successful save for a
key wins and later saves of that key do not replace it.

Bazel is configured to cache outputs via its separate remote cache, but still
depends on GitHub Actions caching for its repository inputs.

The Go module and Bazel dependency caches restore their exact key first and may
restore a same-platform prefix as a warm start. An exact hit skips preparation.
A non-exact hit or miss prepares the input locally. For Go and Bazel dependency
caches, the workload then runs with downloads disabled, so a missing input fails
instead of being silently downloaded later.

Only `github.ref == 'refs/heads/master'` can save an input cache. Pull request,
merge-queue, tag, and non-`master` branch runs can restore entries and prepare a
local miss, but cannot save it. The local
[`cache-policy`](../.github/actions/cache-policy/action.yml) action is where this
policy is defined. Shared cache setup actions serve both cache-writing setup
jobs and restore-only consumers. Only a designated cache-writing setup job uses
`request-cache-save` to request permission to save. All other uses restore and
prepare inputs without saving. The policy action returns `cache-save-allowed`
only when that request is permitted.

GitHub isolates pull-request cache entries from `master`, and cache entries are
immutable. Thus, a pull-request cache cannot replace or supply a `master` cache
entry. The restriction on writes instead protects the repository's limited
cache storage. High pull-request traffic can evict useful entries from `master`
and cause repeated cache misses. Restricting writes to `master` reserves cache
storage for merged code and makes cache usage predictable.

GitHub's
[`cache-mode: read`](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#controlling-cache-access-with-cache-mode)
can prevent a job from saving caches, but it is set at the workflow or job
level, before a cache setup action runs. Using it to implement this policy
would repeat the save decision across workflow jobs. Instead, `cache-policy`
makes that decision once for the Go, Bazel, Nix, and Task cache actions. The
same actions restore and prepare inputs on every ref; only permitted setup
jobs save. This also provides one place to add a temporary exception when a
pull request needs to test cache writes.

To validate cache saves before merge, add a temporary exception for that pull
request to `cache-policy`. This confines write permission to one reviewable pull
request instead of adding a workflow input or general rule that could let
unrelated pull requests consume cache storage. Delete the exception before
merging so the policy remains limited to normal `master` writes.

For example, add the pull-request test as the second allowed branch in
`cache-policy`:

```bash
[[ "$GITHUB_REF" == 'refs/heads/master' ]] || {
  [[ "$GITHUB_EVENT_NAME" == 'pull_request' ]] &&
  [[ "$PULL_REQUEST_NUMBER" == '<pull-request-number>' ]]
}
```

Replace `<pull-request-number>` for the validation run, then delete that
conditional branch before merge. After validation, delete cache entries created
by the temporary exception from [GitHub Actions
caches](https://github.com/ava-labs/avalanchego/actions/caches). The entries
created for the PR will be marked with `refs/pull/[PR number]/merge` instead of
`master`.

#### Event behavior

All jobs restore matching input caches when available.

- **Pull request, merge queue, tag, or non-`master` branch:** jobs do not save
  shared entries. A consumer prepares any missing input on its own runner.
- **Post-merge `master`:** designated setup jobs prepare and save shared entries
  before their dependent jobs run.
- **Scheduled `master`:** setup jobs also save entries, including entries for
  platforms that post-merge CI does not run.

A job cannot transfer locally prepared cache input to another GitHub-hosted
runner. On refs where cache writes are denied, each consumer prepares missing or
stale cache inputs on its own runner. On `master`, a setup job saves the prepared
cache entry for dependent jobs to restore.

Concurrent jobs can prepare the same missing or stale cache inputs. GitHub
Actions cache entries are immutable, so the first save wins and later saves
cannot overwrite it.

This policy applies to GitHub Actions input caches. The Bazel remote cache is
separate: normal pre-merge and post-merge Bazel workflows use it when CI has its
URL and authorization header. The scheduled Bazel workflow disables it. See
[Bazel CI external dependency caching](./bazel.md#bazel-ci-external-dependency-caching).

#### Go module cache

[`setup-go-module-cache`](../.github/actions/setup-go-module-cache/action.yml)
restores `GOMODCACHE`. On a non-exact hit it runs
[`download_go_modules.sh`](../scripts/download_go_modules.sh), which resolves
all repository modules and the checked-in
[`go_module_cache_manifest.tsv`](../scripts/go_module_cache_manifest.tsv).
The manifest covers CI tools and pinned module graphs that repository modules
do not reach. After an exact restore or local preparation, the action sets
`GOPROXY=off` before the workload runs. This verifies that the restored or
prepared module set is complete. If the workload needs another module, it fails
instead of downloading it silently; that failure requires updating the
cache-preparation inputs or manifest.

Callers that configure custom polyrepo refs should keep `GOPROXY` enabled during
setup because those refs can require modules that are not represented by the
avalanchego cache key. They must disable the proxy after dependency setup so the
subsequent workload fails if its prepared module set is incomplete.

The cache key has this structure:

```text
go-mod-${runner.os}-${dependency-hash}
```

[`setup-go-module-cache`](../.github/actions/setup-go-module-cache/action.yml)
defines the dependency hash from Go module metadata and cache-preparation inputs.
The module files already contain the Go version. The cache contains module
archives and source files. The key includes `runner.os`, so architectures on the
same operating system share one module cache while cache archives from different
operating systems remain separate.

The platform cache setup jobs in the Go workflows are the only jobs that write
cache entries for Task and Nix. The Linux AMD64 setup job owns the Linux Go
module cache, which is shared across architectures. The macOS setup job owns the
macOS Go module cache. The jobs run their cache-writing steps only when cache
policy permits a save. Other Go workflow jobs depend on the setup job for their
platform. They skip cache-writing steps when policy denies a save, and each
consumer prepares missing cache inputs on its own runner.

The cache-writing setup jobs use
[`setup-go-workflow-cache-producer`](../.github/actions/setup-go-workflow-cache-producer/action.yml).
It requests saves from `setup-go-for-project`, `setup-task`, and `install-nix`
only when `cache-policy` permits cache writing. Other uses of these actions are
restore-only. `install-nix` can use the same module-cache action as a
restore-only consumer. Bazel jobs disable this use because Bazel has a separate
`GOMODCACHE`. The implicit `actions/setup-go` cache is disabled because its
post-job save cannot be limited to `master` runs by `cache-policy`.

#### Bazel dependency cache

[`setup-bazel`](../.github/actions/setup-bazel/action.yml) runs each Bazel
setup job and the `lint-avalanchego` job. It restores the Bazel repository cache,
the Bazel-specific Go module cache, and Bazelisk's downloaded Bazel binary. It
checks metadata only in setup jobs.

Each non-exact restore prepares the checked-in Bazel dependency list. The action
then disables downloads. The setup job checks metadata after downloads are
disabled. On `master`, that job saves the cache after the check succeeds. This
order prevents a metadata check from adding undeclared inputs to a saved cache.
Setup jobs can duplicate this cold-cache work. The action enables
`--repository_disable_download` and sets `GOPROXY=off` for repository rules.
`GOPROXY=off` prevents Gazelle from fetching a missing module.
See [Bazel CI external dependency caching](./bazel.md#checked-in-bazel-dependency-list).

The cache contains external Bazel dependency input and the Bazelisk-downloaded
Bazel binary. It does not contain Bazel build outputs. It is separate from the
Bazel remote action and test-result cache. See [Bazel CI external dependency
caching](./bazel.md#bazel-ci-external-dependency-caching) for its key and dependency
list rules.

#### Nix store cache

`install-nix` restores the Nix store cache on Linux and macOS and loads the
flake dependencies in every job. The platform cache setup jobs in the Go
workflows are the only jobs that write these cache entries. Nix-consuming Go
jobs depend on the setup job for their platform. The setup job saves on
`master`. All other `install-nix` invocations are restore-only consumers. The
flake defines the commands available to Nix jobs, so a command that neither the
runner nor the flake provides fails. Nix can still fetch a missing store path
for a dependency declared by the flake; the Nix store cache only accelerates
that loading. Go jobs that use Nix-provided Go restore the shared Go module
cache through `install-nix` so they reuse the same dependencies as jobs that use
`setup-go-for-project`.  Bazel jobs disable this behavior because they use the
Bazel-specific cache.

#### Changing input caches safely

These rules keep cache production predictable and storage use bounded. They
also ensure that CI fails when preparation omits a required input, rather than
silently downloading it during a later job. This minimizes the potential for
job failure caused by network flakes.

When changing an input cache:

- keep restore enabled on every event type so every job can reuse compatible
  inputs; write permission is a separate policy decision;
- restrict saves to `master` to preserve limited cache capacity for merged
  code;
- keep one designated Go module, Task, and Nix store cache-writing setup job
  for each cache key in a workflow run so dependent jobs wait for one
  preparation and save;
- keep Go module, Task, and Nix store cache-writing setup jobs in the Go
  workflows so the workflow that defines their consumers also owns their
  shared-cache writes;
- keep Bazel dependency cache-writing setup jobs in the Bazel workflows so they
  can prepare the dependency list before their Bazel consumers run;
- prepare every non-exact restore before disabling its network path so a missing
  prepared input fails rather than being silently downloaded later;
- add each new Go tool or pinned module to the module manifest; add each Bazel
  CI target pattern and command-only dependency to the Bazel dependency list;
- do not use a shared cache to transfer build output or test results between
  jobs because those results depend on job-specific configuration and require a
  dedicated transfer protocol.

### Task

[Task](https://taskfile.dev) runs repository operations in CI. The local
[`setup-task`](../.github/actions/setup-task/action.yml) action makes the Task
binary available to Go, Bazel, and Docker jobs. Run this action after checkout
because it reads `tools/external/go.mod`.

CI never compiles Task from source. Every CI path that uses
`./scripts/run_task.sh` must run `setup-task`, `setup-go-for-project`, or
`setup-bazel` first, unless a Nix development shell provides `task`.

See [Task version](./tasks.md#task-version) for the version policy and update
commands.

The action uses a GitHub Actions cache, not an artifact. A cache lets unrelated
jobs and workflow runs reuse one binary. An artifact belongs to one workflow
run. The cache key includes the Task version, operating system, and
architecture.

Before restoring the cache, `setup-task` checks for an executable at
`$RUNNER_TEMP/task/<version>/$RUNNER_OS-$RUNNER_ARCH/task`. If one exists, the
action skips cache restore, download, and save. This lets repeated invocations
reuse Task within a job. For example, both `setup-bazel` and
`setup-go-for-project` invoke `setup-task` when a job needs Bazel and host Go.

Otherwise, the action restores the matching cache. Platform cache setup jobs
in the Go workflows can save a cache entry on `master`. All other jobs are
restore-only consumers. Pull request and merge-queue jobs download Task on a
cache miss. They do not save the binary. This policy reserves cache storage for
merged Task versions. Scheduled `master` jobs can save entries for platforms
not tested on each push.

A shared cache entry exists only after a `master` job runs on the same operating
system and architecture. When you add a CI platform, add a `master` job for that
platform if it needs a reusable Task cache. Otherwise, cache-miss jobs download
Task.

On a cache miss, the action downloads the platform release from the Task GitHub
release and checks its SHA-256 value against the checked-in value in
`setup_task.sh`. This avoids a host Go dependency in Bazel jobs. A source build would
require host Go before Task can run. The checked-in checksum detects a damaged or
changed archive in transit and makes release-archive changes visible in repository
review.

When changing this setup, keep these rules:

- Keep Nix and `tools/external/go.mod` on the same Task version.
- Keep shared cache writes limited to runs on `master`.
- Keep the release platform mapping compatible with every CI runner.

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

### Required-check validation in GitHub

Local lint cannot prove queue admission, check-name preservation, or merge
blocking. After changing required-check workflows, check these properties in
GitHub:

1. Check that branch protection or a ruleset requires the merge queue for each
   target branch. Check that maintainers cannot merge directly while PR checks
   are skipped. Check that branch protection or the ruleset requires the exact
   names listed in
   [Required checks and the merge queue](#required-checks-and-the-merge-queue).
   Check the source app too, if branch protection or the ruleset restricts it.
2. Check that pull-request aggregate jobs skip before their test workflows
   finish. Check that a maintainer can enqueue the pull request while tests run.
3. Check that merge-group aggregates wait for their reusable test workflows.
   Check that successful test workflows produce successful aggregate checks.
4. Check a test failure in each reusable workflow that supplies a required
   check. Check that its merge-group aggregate fails and GitHub blocks merging.
5. Check a cancelled merge-group test workflow. Check that its required
   aggregate does not report success and GitHub blocks merging.
6. Check that only one entrypoint emits each required check for the commit
   under test.

Use a disposable pull request for failure and cancellation checks. Do not merge
its test changes. Record any checks that remain unverified; a lint pass is not
evidence for these GitHub behaviors.
