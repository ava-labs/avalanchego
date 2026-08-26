# CI

This document explains how to maintain this repository's [GitHub
Actions](https://docs.github.com/actions) configuration. These conventions apply
to workflows and [local composite actions](https://docs.github.com/actions/sharing-automations/creating-actions/creating-a-composite-action).

## Table of contents

- [Principles](#principles)
- [How CI is organized](#how-ci-is-organized)
  - [Workflows coordinate repository operations](#workflows-coordinate-repository-operations)
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
- [Test platforms and test configuration](#test-platforms-and-test-configuration)
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
| Go in unified CI | [`setup-go-for-ci`](../.github/actions/setup-go-for-ci/) | A unified Go workflow setup or consumer job needs the prepared workspace dependency cache. |
| Go in other workflows | [`setup-go-for-project`](../.github/actions/setup-go-for-project/) | The job needs Go and does not use unified Go CI, Nix, or Bazel to provide it. |
| Bazel | [`setup-bazel`](../.github/actions/setup-bazel/) | The job needs Bazel, which also provides Go. |
| Flake-provided tools | [`install-nix`](../.github/actions/install-nix/) | A job runs a command that requires a dependency supplied by the Nix dev shell, which also provides Go. |

Outside unified Go CI, `setup-go-for-project`, `setup-bazel`, and `install-nix`
are alternative Go provisioning mechanisms. A job that uses `setup-bazel` can
also use `install-nix` for dependencies that Bazel does not provide.

`install-nix` keeps its standalone Go caches off by default, so two cache
actions do not restore or save the same `GOMODCACHE` or `GOCACHE` paths. A job
that has no other source of Go dependencies turns them on with `cache_go`. Two
jobs do that today: the Bazel e2e smoke tests, which build xsvm and ginkgo with
plain `go` rather than Bazel. When both become Bazel targets, remove those
opt-ins, and then `cache_go` has no callers left and can be deleted with the
steps it gates.

Docker-only image builds remain independent because their module downloads occur
inside Docker build layers, not in the runner's prepared `GOMODCACHE`.

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

## Test platforms and test configuration

[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) is the unified
non-Bazel Go pre-merge entrypoint for avalanchego, Coreth, EVM, and Subnet-EVM.
It runs for pull requests, merge groups, pushes to `master` and `dev`, and tag
pushes. It calls the reusable full workflow for Ubuntu 24.04 AMD64 and the
reusable smoke workflow for macOS 26 ARM64. The full workflow also contains the
four suites' pre-merge lint, generation, image, E2E, load, and upgrade jobs.

[`.github/workflows/go-ci.yml`](../.github/workflows/go-ci.yml) accepts a runner
and platform name. Each invocation prepares dependencies once and then runs the
four module-specific unit suites and one combined workspace suite. The combined
suite runs beside the module suites so CI can compare their cold and warm costs
before replacing the four-job fan-out. Its `run_race_shuffle_unit_tests` input
selects the cacheable or race/shuffle task variant. Its `run_premerge_jobs`
input prevents pre-merge-only jobs from running on every scheduled platform.
[`.github/workflows/go-ci-smoke.yml`](../.github/workflows/go-ci-smoke.yml)
uses the same runner and platform inputs and fans out the four
`test-unit-smoke` tasks.

Pre-merge CI uses fewer test platforms to reduce failures from hosted runners.
It runs the four full cacheable module suites and the combined workspace suite
on Ubuntu 24.04 AMD64. It runs one small smoke test from each module on macOS 26
ARM64. The smoke tests prove that Go can build and run tests on macOS. They do
not provide full macOS coverage.

[`.github/workflows/ci-scheduled.yml`](../.github/workflows/ci-scheduled.yml)
is the single daily non-Bazel Go entrypoint. It calls the reusable full workflow
for Ubuntu 22.04 and 24.04 on AMD64 and ARM64, and for macOS 26 ARM64. Every
scheduled invocation runs the four module suites and the combined workspace
suite with race detection and shuffled order. Scheduled workflows stay separate
from pull-request and merge-group
workflows so scheduled-only jobs do not appear as skipped checks.

Each reusable workflow invocation runs its setup jobs before its Go jobs fan out.
The setup job uses `scripts/download_go_dependencies.sh` to download the
workspace build list, including dependencies used only by tests. It then
downloads each module's own build list with `GOWORK=off`, because lint tooling,
the per-module suites, and `go mod tidy` all resolve per module and can select
versions the workspace resolution does not. It separately downloads the
repository Go-tool module graph because `tools/external` is not in the
workspace, and installs the tools pinned in `scripts/lib_go_tools.sh`, which are
invoked as `go run pkg@version` and resolve a module graph of their own. It saves
one platform-specific workspace `GOMODCACHE`. Consumer jobs restore that cache
read-only. The cache key covers the Go version source, workspace files, every
listed module file, and the dependency-download implementation.

A workflow with no setup job sets `initial_setup` on the job that needs the
dependencies. That job populates and saves its own cache.
`firewood-chaos-test`, `firewood-load-test`, and `self-hosted-load-tests` work
this way. The last two share a cache key, because they name the same runner, and
the key is immutable: the job that finishes first populates it, and the other
skips its save.

The setup job also builds the `task` binary from `tools/external` and shares it
through a small platform-specific cache. Consumer jobs restore it and put it on
`PATH`. Without it every job that does not already have `task` on `PATH` rebuilds
it, which measured about 15 seconds per job, and only the cacheable test jobs
have a `GOCACHE` that could absorb that cost. The cache holds the binary rather
than the `GOCACHE` that produced it: about 13MB instead of about 150MB, and no
second cache restoring into the `GOCACHE` path this action already manages. The
key has no restore prefix, because a binary built from a different
`tools/external` or Go version must not be reused. A miss is not a failure;
`scripts/run_task.sh` still builds `task` itself.

The full workflow runs a second setup job, `setup-blacksmith`. Blacksmith
runners redirect the Actions cache API to their own colocated cache instead of
GitHub's backend, so a cache saved on a GitHub-hosted runner is invisible to
them and vice versa. Without a Blacksmith-side setup job, every Blacksmith job
misses the dependency cache and downloads its whole module graph. That job uses
the smallest Blacksmith instance because it only downloads, and it is gated on
`run_premerge_jobs` because every Blacksmith job in this workflow is pre-merge
only. When you move a job onto or off a Blacksmith runner, move its `needs`
between the two setup jobs at the same time.

The `c-chain-reexecution` job runs the C-Chain re-execution benchmark for pull
requests. It shares this workflow's setup job, so the repository keeps one Go
dependency cache instead of two. It sets the benchmark action's
`manage-go-caches` input to `false`, because two caches that save the same
directory compete.

The `c-chain-reexecution-benchmark-*` workflows run the other benchmark
configurations, on dispatch and on a schedule. Those runs keep
`manage-go-caches` set to `true`. They also use Blacksmith runners and other
self-hosted runners. The setup jobs here cannot reach those caches. The Bazel
workflows do not use Blacksmith runners.

`go-required` does not include `c-chain-reexecution`. GitHub skips that job for
pull requests from forks and Dependabot, because the benchmark action assumes
access to an AWS role identifier secret and GitHub OIDC permission to assume
that role. Those events do not receive the secret. `go-required` treats a
skipped pre-merge job as a failure.

Do not use `c-chain-reexecution` as a required branch-protection check. It is
skipped when the AWS role is unavailable, so it cannot enforce the benchmark for
every pull request.

Every job that uses `setup-go-for-ci` then runs with `GOPROXY=off`, so every
module it needs must already be in the cache. A miss fails the job and names the
missing module. A silent download would instead hide an incomplete
dependency-download script and pay the download cost in every job. A job that
populates its own cache is checked the same way, because the check is applied
after its download. Jobs that resolve module versions the download script cannot
predict, such as the `go mod tidy` checks, set `allow_dependency_download` on
`setup-go-for-ci`.

Dependency and test-result caching are separate. Each cacheable pre-merge full
or smoke job manages its own suite- and platform-specific `GOCACHE`. The primary
key includes `github.sha`, and a platform/suite prefix supplies a warm start from
an earlier revision. Scheduled race/shuffle jobs do not restore or save
`GOCACHE`, so their tests execute on every run.

### Go cache lifecycle and trust boundary

GitHub Actions scopes caches by Git ref. A pull request can restore matching
caches from its base branch, but a cache saved by the pull request remains in
the pull request's merge-ref scope. It cannot replace a cache used by `master`,
another branch, or another pull request. Merging a pull request does not promote
its caches. The post-merge `master` workflow must succeed and save its own cache
before later pull requests can restore the merged cache state. Cache actions
save only after their job succeeds, and cache entries are immutable for a given
key.

For `GOCACHE`, a new pull request revision normally misses the primary key
because it contains the pull request merge SHA. The restore prefix then selects
a recent accessible cache for the same suite and platform, normally from an
earlier revision of that pull request or from `master`. Go, not the workflow,
decides package-level reuse. It reuses only successful test results whose
compiled test inputs, dependencies, cacheable flags, relevant environment, and
observed file inputs still match. Changed packages and packages affected by
changed dependencies run again. The job saves the resulting cache under its new
revision-specific key.

For `GOMODCACHE`, unchanged workspace dependency inputs produce an exact key
match. Setup still runs the complete dependency-download command, which should
find all required modules locally. If module inputs change, setup restores a
recent same-platform cache as a warm start, downloads the missing module
versions, and saves the completed cache under the new dependency key. Old module
versions can remain in the cache; avoiding repeated downloads takes priority
over minimizing cache size. Consumer jobs use `actions/cache/restore`, so they
cannot save a partial or competing dependency cache.

Pull requests can read base-branch caches. Never put credentials, private source,
or other secrets in either Go cache. This repository caches public Go modules
and derived build/test artifacts only. The workflows use `pull_request`, not the
privileged `pull_request_target` event, and protected secrets are not available
to untrusted pull request jobs. A pull request can affect cache contents within
its own scope, but those entries do not become trusted-branch caches.

`task check-go-mod-tidy` also validates workspace checksums, and its fix is
`task go-mod-tidy`. After `go work sync`, the fix runs `go mod download` and
`go list -m all` to record the checksums that `go work sync` leaves out. This
works around [Go issue #63901](https://github.com/golang/go/issues/63901),
where `go work sync` can leave `go.work.sum` incomplete. After adopting a Go
release with that fix, test whether the extra commands can be removed.

Every command that writes `go.work.sum` or a member `go.sum` belongs in that
task. Warm-up steps elsewhere must leave the checked-in checksums untouched: a
dirty tree breaks any later step that switches branches. The C-Chain benchmark
comparison step switches to the gh-pages branch to read stored results, whether
or not the job publishes.

When reviewing or changing this implementation:

- keep dependency keys tied to the Go version source, every workspace and tool
  module file, and the dependency-download script and action
- keep every Blacksmith job on `needs: setup-blacksmith` and every
  GitHub-hosted job on `needs: setup`; the two caches do not reach each other
- keep `scripts/download_go_dependencies.sh` covering every resolution CI uses:
  the workspace build list in workspace mode, each module's build list with
  `GOWORK=off`, `tools/external`, and the versioned tools in
  `scripts/lib_go_tools.sh`
- add a tool to `tools/external` where Bazel allows it; pin it in
  `scripts/lib_go_tools.sh` only when it cannot go there, and say why
- keep dependency setup as the only `GOMODCACHE` writer and keep consumers on
  restore-only behavior
- keep `GOCACHE` keys revision-, suite-, and platform-specific, with a
  suite/platform restore prefix
- do not enable `GOCACHE` restore or save for scheduled race/shuffle tests
- do not allow `install-nix` or a nested action to restore a second cache into a
  path already managed by `setup-go-for-ci`
- keep consumer jobs on `GOPROXY=off`; set `allow_dependency_download` only for
  a job that must resolve module versions the setup job cannot predict
- keep the `task` binary cache keyed exactly, with no restore prefix, and keep a
  cache miss non-fatal
- verify cold, warm, same-revision, post-merge `master`, and scheduled behavior
  in CI logs after changing cache keys or scope

The top-level `go-required` job replaces `tests-required`, `coreth-required`,
`evm-shared-required`, and `subnet-evm-required`. Branch protection must remove
the four old checks only after a pull request shows the exact displayed name of
the new top-level check and confirms that it fails when either reusable call
fails or is skipped. This repository-setting migration cannot be completed in
workflow code.

The migration retains these job destinations:

- avalanchego full workflow: `Fuzz`, `e2e`, `e2e_schedule_latest`,
  `e2e_post_latest`, `e2e_kube`, `e2e_existing_network`, `Upgrade`, `Lint`,
  `tausecondslint`, `links-lint`, `check_generated_protobuf`, `check_mockgen`,
  `check_canotogen`, `check_contract_bindings`, `check_go_mod_tidy`,
  `test_build_image`, `test_build_antithesis_avalanchego_images`,
  `e2e_bootstrap_monitor`, `load`, `robustness`, and `c-chain-reexecution`
- Coreth full workflow: `lint-coreth` and `e2e-warp-coreth`
- EVM full workflow: `lint-evm`
- Subnet-EVM full workflow: `lint-subnet-evm`, `e2e-warp-subnet-evm`,
  `e2e-load-subnet-evm`, `test-build-image-subnet-evm`, and
  `test-build-antithesis-images-subnet-evm`
- reusable full workflow: `unit-all`, `unit-avalanchego`, `unit-coreth`,
  `unit-evm`, and `unit-subnet-evm`, including all four former scheduled
  matrices
- reusable smoke workflow: `smoke-avalanchego`, `smoke-coreth`, `smoke-evm`,
  and `smoke-subnet-evm`

`load_kube_kind` remains commented out. Do not enable it while changing this
workflow structure.

The combined workspace suite must run with the Go workspace enabled. It loads
packages from every workspace module in one `go test` invocation, so it depends
on `go.work`. With the workspace off, the graft modules resolve through the root
`go.sum`, which does not carry their transitive checksums, and the suite fails
with `missing go.sum entry`. The module-specific suites are unaffected because
each runs from inside its own module directory.

Keeping the workspace enabled is not automatic. `scripts/run_tool.sh` needs
`GOWORK=off` for `go tool -modfile`, and `go tool` passes that assignment to the
tool it launches. When `task` is not on `PATH`, which is the case for every CI
job, `scripts/run_task.sh` reaches task through that path. It therefore resolves
the task binary with `go tool -n` and executes it as a separate step, so
`GOWORK=off` stays with the build and task runs its commands in the caller's
environment. `scripts/test_run_task_launcher.sh` covers this. Do not reintroduce
a launcher that execs task through a `GOWORK=off` assignment.

When you change a test platform or test configuration, update this policy and
the related workflow and task entrypoints together.

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
