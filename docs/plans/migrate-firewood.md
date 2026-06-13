# Migrate Firewood to Avalanchego

The goal is to migrate github.com/ava-labs/firewood into
github.com/ava-labs/avalanchego at path `./firewood`. Combined with bazelification,
this enables building avalanchego with firewood compiled from source.

This document catalogs all known issues related to the migration so the firewood team
can understand the scope, evaluate risks, and make choices — including what to address
before vs after the initial cutover.

## Approach

The migration uses a git subtree merge to graft the full firewood history into
avalanchego. Bazel then compiles the Rust code from source via `rules_rust` and
`crate_universe`, replacing the pre-built static library dependency. CI workflows are
renamed and adapted to run from the monorepo.

This is being delivered across several PRs:

- [#5027](https://github.com/ava-labs/avalanchego/pull/5027) — Subtree import of
  firewood. Periodically updated as the firewood repo receives changes up until cutover.
  This PR proves the repository move itself: importing firewood under `firewood/` as a
  git subtree while preserving upstream history and lineage metadata.
- [#5050](https://github.com/ava-labs/avalanchego/pull/5050) — Build firewood from
  source with Bazel. It replaces the pre-built `firewood-go-ethhash/ffi` Bazel patching
  model with in-tree Rust builds via `rules_rust` and `crate_universe`, using
  hand-written `BUILD.bazel` files for the 5 runtime crates needed by avalanchego. It
  also adds lint exclusions so avalanchego linters skip `firewood/`, a Rust toolchain
  version check (`scripts/check_rust_version.sh`), and leaves the remaining firewood
  workspace crates outside the initial Bazel graph. This PR proves the hardest
  technical integration point: building and testing the Go/C/Rust FFI from source
  inside avalanchego.
- [#5079](https://github.com/ava-labs/avalanchego/pull/5079) — Migrate firewood CI
  workflows into the monorepo. It adapts firewood's GitHub Actions to avalanchego's
  layout, adds `firewood/**` CODEOWNERS entries and Cargo dependabot configuration, and
  identifies the workflows that remain deferred or obsolete in the monorepo model,
  including GH Pages, performance tracking, PR title enforcement, the golangci diff
  check, and nix-based FFI builds. This PR proves the workflow, governance, and
  repository-administration migration.
- TBD — Refactor firewood's cross-repo benchmarking jobs (not yet started).

These PRs are intended to prove out and derisk the migration plan. They should be read
as validation work for the approach, not as assumptions about what has already landed.

Taken together, these PRs validate the migration as three separable concerns:

- subtree import and history preservation
- source-built Rust FFI integration
- CI, governance, and repository administration migration

The remaining workspace crates (replay, triehash, fwdctl, benchmark) are dev/tooling
crates not required by the Bazel build. The firewood team may want CI coverage for them
post-migration.

Once the PRs land, these capabilities will be in place:

- Bazel FFI builds: Rust compiled from source, Go CGO integration
- Daily load and chaos tests running in avalanchego CI
- Lint isolation: avalanchego linters skip `firewood/`; firewood lint runs separately
- Rust toolchain sync check enforced via `task lint-all`
- CODEOWNERS routing PRs that touch `firewood/**` to the firewood team
- Dependabot monitoring for Cargo dependencies

## Day-to-Day Development

### Review Process

Avalanchego currently requires 2 approvals per PR. Firewood PRs today require 1
approval within 1 business day (per CONTRIBUTING.md).

- Need to confirm whether `firewood/**` CODEOWNERS approval already satisfies
  avalanchego's approval policy for firewood-only changes
- If not, decide whether policy changes are needed so firewood-only changes do not
  require unnecessary extra approvals
- **Decision needed:** Will the current approval policy slow the firewood team down
  enough to warrant changing it?

### Breaking Changes Across the FFI Boundary
!! Isn't there more detail here?

Post-migration, firewood changes that break avalanchego will block the PR since
avalanchego CI will fail.

- Rust-only changes (no FFI signature change) are low risk — they either compile or don't
- FFI signature changes (new/modified `pub extern "C"` functions) are high risk — they
  require coordinated Go wrapper updates
- Need a protocol for how the firewood team communicates upcoming breaking changes
- Consider a CI job that builds only the FFI boundary for fast signal on breakage

### PR Title Format

Firewood enforces conventional commits via `amannn/action-semantic-pull-request`
(allowed types: build, chore, ci, docs, feat, fix, perf, refactor, style, test).
Avalanchego does not enforce PR title format.

**Decision needed:**
- Drop the requirement (simplest)
- Scope the check to only run when `firewood/**` files are changed
- Adopt conventional commits repo-wide (bigger lift)

### Go Module Isolation and `go.work`
!! This seems vague - what is the actual impact?

The firewood FFI has its own `go.mod` files (`firewood/ffi/go.mod`,
`firewood/ffi/tests/eth/go.mod`, `firewood/ffi/tests/firewood/go.mod`) that are
independent of avalanchego's module. #5050 uses `GOWORK=off` in CI to prevent Go
workspace mode from pulling these into the main build.

Developers running `go test` locally in firewood FFI directories need to be aware of
this — the root `go.work` file does not include these modules, and adding them would
break the main build.

#### Requirements and Options

Observed behavior from local validation on 2026-03-24:

- `GOWORK=/path/to/overlay.work` can redirect module resolution to local workspace
  members outside the committed repo `go.work`.
- `GOWORK=""` from the repo root still uses the committed `./go.work`, and
  `GOWORK=off` still disables workspace mode entirely.
- `gopls` follows the alternate workspace: `gopls check` succeeded with an overlay that
  added an extra module and failed outside workspace mode for the same setup.
- Repo-local package tests in the root module still passed in overlay, committed
  workspace, and `GOWORK=off` modes.
- These experiments validate the overlay mechanism itself. They do not yet validate the
  exact `firewood/ffi/go.mod` layout described here, because those module files are not
  present in this worktree snapshot.

Requirements for the chosen strategy:

- **CI must be able to validate coordinated avalanchego+firewood changes before
  publication**: Bazel must be the authoritative integration path for source-built
  firewood changes.
- **Ordinary raw-Go workflows still need a supported path**: the published
  `github.com/ava-labs/firewood-go-ethhash/ffi` module remains the compatibility layer
  for non-Bazel workflows and downstream `go get` consumers.
- **The raw-Go gating surface should be intentionally small**: when a backward-
  incompatible firewood change is needed, the repo should be able to temporarily disable
  a small, explicit set of published-module-based gates rather than a large fraction of
  the CI system.
- **Backward-compatible changes should remain non-disruptive**: the exceptional
  disable/publish/bump/re-enable flow is only for intentionally incompatible FFI
  changes.
- **Process overhead should be minimized**: avoid staging branches or publication-before-
  merge choreography as the default answer.

Chosen strategy:

- **Bazel has primacy** for the firewood-integrated build and test path. The in-tree
  `./firewood` source is authoritative for integration testing and for catching
  regressions introduced by firewood changes.
- **Raw-Go support is preserved through the published module**. The repo continues to
  support pure-Go workflows against `github.com/ava-labs/firewood-go-ethhash/ffi`, but
  only for a deliberately minimal set of workflows.
- **Backward-incompatible firewood changes use an exception flow**:
  1. temporarily disable the small set of raw-Go / published-module-based gating jobs
  2. merge the in-tree firewood + avalanchego change with Bazel coverage still active
  3. publish an updated `firewood-go-ethhash/ffi` module
  4. bump avalanchego to the newly published version
  5. re-enable the raw-Go gating jobs

This implies two supported consumption modes:

- **Bazel / in-tree-source mode:** authoritative for CI, integration, and validation of
  firewood changes against avalanchego
- **Published-module mode:** authoritative for raw-Go workflows and downstream `go get`
  consumers at coordinated release boundaries

Rejected as the primary answer:

- **Developer-local `GOWORK` overlays:** useful as an optional local convenience for
  joint avalanchego+firewood iteration, but not sufficient as the repo-wide answer.
  They do not solve CI or default post-merge workflows by themselves.
- **Adding firewood FFI modules to the committed root `go.work`:** would redefine the
  repo-wide workspace contract used by Bazel, `go work sync`, version checks, and
  default IDE behavior.
- **Staging branch / publish-before-merge choreography:** preserves the main branch's
  raw-Go workflow more strictly, but adds ongoing branch and release-management process
  that the project would prefer to avoid unless the lower-process strategy proves
  unworkable.
!! avalanchego uses the master branch, not main
!! It should be clear that publication is at release boundaries. releases are required for breaking changes, but can also occur for non-breaking changes
- **Coordinated dev/pre-release FFI publication before merge:** could keep ordinary
  raw-Go workflows green on the main branch with less long-lived branch management than
  a staging-only model, but it still adds publication choreography to the normal merge
  path. The chosen strategy instead keeps publication as an exception path for breaking
  changes only.

### Development Tooling

!! The firewood team is responsible for their tooling since it only affects them. Not a migration consideration.

Firewood uses tools not present in avalanchego's dev environment:
- `just` (justfile task runner) — 10KB of recipes for builds, tests, releases, benchmarks
- `git-cliff` — changelog generation from conventional commits
- `cargo-edit` — automated Cargo.toml version bumping
- `cbindgen` — C header generation (currently committed, not build-time)
- `cargo-nextest` — test runner used in CI
!! Nix flake is superceded by bazel so nix flake and everything associated with it can be removed
- `nix` flake for FFI builds (`ffi/flake.nix`)

**Decision needed:** Add these to avalanchego's dev environment, or phase them out in
favor of avalanchego equivalents (Taskfile, Bazel, etc.)?

### Follow-Up Required: Reduce the Raw-Go Gating Surface

The current `Taskfile.yml` surface area is broader than the desired end state for this
strategy. Today, breaking firewood changes would affect more raw-Go workflows than we
want to carry as temporary exceptions.

To make the disable/publish/bump/re-enable flow safe and low-friction, follow-up work
is required to reduce the authoritative raw-Go gating surface to a small, explicit list.
The intended end state is:

- Bazel is authoritative for firewood-integrated build and test coverage
- published-module / raw-Go gates are minimal and explicitly enumerated
- ideally, the remaining raw-Go gate surface is close to unit-test-oriented coverage

Areas that currently still depend on raw Go and may need additional Bazel coverage or
explicit exemption decisions include:

- **Core build and test loops**: `build`, `build-race`, `test-unit`, `test-unit-fast`,
  `test-e2e`, `test-e2e-ci`, `test-e2e-existing-ci`, `test-upgrade`, and several
  antithesis tasks all compile or test Go binaries through `go build`, `go test`, or
  `go run`, either directly in `Taskfile.yml` or via scripts such as
  `scripts/build.sh` and `scripts/build_test.sh`.
- **Auxiliary binaries used in workflows**: `build-bootstrap-monitor`,
  `build-tmpnetctl`, `build-xsvm`, and `build-subnet-evm` all compile standalone Go
  binaries or plugins outside the main avalanchego binary.
!! I'm not sure i understand these issues. what does it have to do with firewood dependency? these all generate files that need to be committed to the tree to enable IDE and downstream usage.
- **Code generation**: `generate-canoto`, `generate-load-contract-bindings`,
  `generate-mocks`, and `generate-protobuf` depend on `go generate` or Go-specific code
  generators. A Bazel-first workflow would need equivalent generation entrypoints or a
  deliberate split where generation remains a raw-Go workflow.
!! These areguably have nothing to do with bazel. I guess the point though is that some of them would be broken by breaking firewood changes? certainly go-mod-tidy?
- **Module and workspace maintenance**: `go-mod-tidy`, `sync-go-work`,
  `check-go-version`, `check-require-directives`, `check-require-directives-round-trip`,
  `update-go-version`, and `tags-update-require-directives` all operate on `go.mod` or
  `go.work` state. These are not replaced by Bazel automatically; they express repo
  metadata and release-management workflows.
!! What would be the impact of a breaking firewood change on golangci-lint and CI jobs that run it?
- **Lint and static analysis**: `lint`, `lint-fix`, and part of `lint-all` use
  golangci-lint and other Go-aware checks through `scripts/run_tool.sh`. A Bazel-only
  build/test posture would still need a story for Go linting and repo policy checks.
!! load and cchain- and export- would seem things that could rely on bazel instead easily enough.
!! What would the impact be of a breaking firewood change on test-fuzz?
- **Load, reexecution, fuzz, and benchmarking tools**: `test-load*`,
  `test-cchain-reexecution`, `test-fuzz*`, and `export-cchain-block-range` invoke Go
  programs with `go run` or `go test -fuzz`. These are user-facing workflows, not just
  implementation details.
!! Presumably all this image building could be replaced by bazel-driven image building?
- **Image and packaging paths with Go dependencies**: `build-image`,
  `build-xsvm-image`, `build-antithesis-images-*`, and `.github/packaging/Taskfile.yml`
  query Go module metadata such as the repo's Go version, and some of the antithesis
  image flows also run Go-based compose/config generators during image assembly.
!! Polyrepo was to enable cross-repo testing pre-migration and won't need to exist afterwards
!! What other tasks require go and what would the impact of a breaking firewood change on them?
- **Polyrepo and release helpers**: `run-polyrepo` and some tag/update tasks rely on Go
  commands as orchestration tools for dependency and release workflows.

The current CI impact is therefore not hypothetical. If firewood made a backward-
incompatible FFI change today and the published `firewood-go-ethhash/ffi` module lagged
behind, the following jobs would be the most relevant breakage surface:

- **Directly exposed core gates**:
  - `Tests / Unit` in `.github/workflows/ci.yml` via `task test-unit`
  - `Coreth / unit_test` in `.github/workflows/coreth-ci.yml`
  - likely `Tests / Lint` in `.github/workflows/ci.yml`, because `golangci-lint`
    type-checks against the raw Go module graph
!! all these jobs could be converted to bazel
- **Likely exposed integration gates**:
  - `e2e`
  - `e2e_post_latest`
  - `e2e_existing_network`
  - `Upgrade`
  - `load`
  - `load_kube_kind`
!! Build and publication jobs could almost certainly be updated to use bazel
  These still build or run avalanchego through raw-Go `build` / `build-race` /
  `go run` paths rather than Bazel-built artifacts.
- **Ancillary but definitely vulnerable workflows**:
  - release / packaging jobs such as `build-linux-release`, `build-macos-release`, and
    `publish_docker_image`
!! These are slated for bazelification as part of PR 5050
  - firewood-specific `firewood-load-test` and `firewood-chaos-test`
!! This should be bazelifified
  - the C-Chain reexecution benchmark workflows

The most important mitigating fact is that `bazel-ci.yml` already provides Bazel-
authoritative unit and e2e coverage for the main module, coreth/evm, and subnet-evm.
That means the follow-up is not "Bazelize everything before migration." It is more
targeted:

- **Smallest critical surface**: make `bazel-ci` the authoritative presubmit signal and
  treat raw-Go `Unit`, `Coreth unit_test`, and probably raw-Go `Lint` as the main jobs
  that matter during a breaking-change exception
- **Medium effort**: move the raw-Go e2e/load family to consume Bazel-built binaries,
  similar to the existing Bazel e2e path
- **Larger but lower-priority effort**: make release/image/subnet-evm packaging jobs
  Bazel-driven, or explicitly keep them out of the critical gate surface during an
  exception window
- **Lowest priority**: scheduled firewood load/chaos/reexecution/benchmark workflows,
  which can be temporarily exempted more easily than presubmit gates

This breakdown is included primarily so non-firewood stakeholders can assess scope. The
remaining work appears moderate rather than open-ended: there is a small mandatory
presubmit surface, a medium-sized integration tail, and a larger ancillary tail that
does not need to block the migration decision.

This follow-up can happen either before the firewood migration lands or later, but it
must happen before the first backward-incompatible firewood change makes the exception
workflow operationally critical.

## CI & Quality Gates

### Workflow Migration Status

#5079 migrates the bulk of firewood CI by renaming workflows from
`firewood/.github/workflows/*.yaml` to `.github/workflows/firewood-*.yml` and adapting
them for the monorepo (path filters, working directories, artifact prefixes):

**Migrated:**
- `ci.yaml` → `firewood-ci.yml`
- `benchmarks.yaml` → `firewood-benchmarks.yml`
- `attach-static-libs.yaml` → `firewood-attach-static-libs.yml`
- `cache-cleanup.yaml` → `firewood-cache-cleanup.yml`
- `default-branch-cache.yaml` → `firewood-default-branch-cache.yml`
- `label-pull-requests.yaml` → `firewood-label-pull-requests.yml`
- `metrics-check.yaml` → `firewood-metrics-check.yml`
- `publish.yaml` → `firewood-publish.yml`
- `release.yaml` → `firewood-release.yml`

**Deferred (blocked on decisions):**
- `gh-pages.yaml` — blocked on GH Pages decision (see [Publishing & GH Pages](#publishing--gh-pages))
- `track-performance.yml` — blocked on benchmark data branch decision
- `pr-title.yaml` — blocked on PR title format decision (see above)
- `expected-golangci-yaml-diff.yaml` — validates that Go lint config diffs match
  expected deviations from upstream; needs evaluation for whether it's still relevant
- `ffi-nix.yaml` — nix-based FFI builds, may be superseded by Bazel

### Rust-Specific CI

The multi-platform Rust CI (`ci.yaml`) covers: build, rustfmt, clippy, nextest, docs,
differential fuzzing, and stale dependency checks. Bazel covers build+test for the FFI
crates, but pure-Rust quality gates (clippy, rustfmt, docs, fuzzing) have no Bazel
equivalent.

!! They don't need bazel equivalents, now and possibly ever. The current CI is intended to be maintained separately. Only the ffi layer enabling integration will be targeted by bazel initially.

### Lint Configuration

Firewood maintains its own lint configs that differ from avalanchego:

- **Go linting:** `firewood/ffi/.golangci.yaml` enables different linters than
  avalanchego's root config (e.g., embeddedstructfieldcheck, godoclint, modernize;
  removes avalanchego-specific rules like container/list, utils imports). Note: this is
  a fully independent config, not a relaxed subset like `graft/.golangci.yml`.
- **Rust linting:** `firewood/clippy.toml` with custom disallowed methods/types
  (parking_lot over std::sync, seeded RNG, etc.)
- **License headers:** Firewood checks `.rs`, `.go`, `.h` files with its own template
  (`.github/firewood-check-license-headers.yaml`)

!! Invisible how? CI jobs will enforce firewood lint rules if an avalanchego developer inadvertendly violates a rule
Avalanchego linters will exclude `firewood/` via #5050 (`scripts/lint.sh`,
`scripts/shellcheck.sh`, `scripts/lib_go_modules.sh`), so firewood lint runs
separately through its own migrated CI workflows. This is intentional but means
firewood quality gates are invisible to avalanchego-focused developers.

### Quality Gates Unique to Firewood

These firewood-specific checks migrate with #5079:
- License header checks (viperproject/check-license-header)
- Metrics change detection — warns if metric changes lack Grafana dashboard updates
- markdownlint-cli2 for docs
- Cache cleanup on PR close

!! This should be maintained, the firewood team owns it.
The golangci-lint diff validation (`expected-golangci-yaml-diff.yaml`) is deferred —
its purpose is to ensure the Go lint config tracks expected deviations from an upstream
baseline, and it may need rethinking in the monorepo context.

### Rust Toolchain Synchronization

The Rust version and edition must match between `firewood/Cargo.toml` and
`MODULE.bazel`. A CI check (`scripts/check_rust_version.sh`, wired into
`task lint-all` via #5050) validates this.

!! So long as this is validated by CI, why is this a concern?'t this validated in CI?
- MSRV bumps in firewood require a coordinated `MODULE.bazel` update — easy to forget
- **Decision needed:** Can the firewood team bump MSRV independently, or does it
  require avalanchego team sign-off since it affects the Bazel toolchain?

### Supply Chain Security

!! Why is this a migration concern?
Firewood does not currently use `cargo-deny` (no `deny.toml` exists in the repo).

**Decision needed:** Should firewood's Cargo dependencies get supply-chain auditing
(license allowlist, advisory database checks, ban list)? Avalanchego has its own
dependency security posture — decide whether to extend it to Cargo.

### Dependency Management

Firewood's dependabot config (Cargo security-only daily, GitHub Actions weekly) will be
merged into avalanchego's dependabot via #5079. Note that `Cargo.lock` (~136K) will
generate large diffs on dependency updates.

## Release & Distribution

### Crates.io Publishing

!! No change, firewood will continue to operate as before
Firewood publishes crates to crates.io independently. Post-migration:

- **Decision needed:** Continue publishing from the subtree?
  - If yes: release workflow needs updating to work from `firewood/` subdirectory
  - If no: downstream Rust consumers need an alternative
- Version bumps are currently manual (Cargo.toml edits + git-cliff changelog). The
  process (documented in `firewood/RELEASE.md`) requires `cargo-edit`, `git-cliff`, and
  `just` — tools not in avalanchego's dev environment.
- **Decision needed:** Does firewood maintain independent versioning, or do avalanchego
  releases bundle a firewood version?

### Static Library Publishing

!! Yes continue publishing, not even a question. This enables both downstream consumers and pure golang development workflows
The `firewood-go-ethhash` repo receives pre-built `.a` files via
`attach-static-libs.yaml`, triggered on tag push. External Go consumers who don't use
Bazel depend on this.

- **Decision needed:** Continue publishing? Automate from avalanchego CI?
- The workflow needs updated tokens and permissions post-migration (currently uses
  `FIREWOOD_GO_GITHUB_TOKEN`)

### Git Tags

Firewood tags follow `v*.*.*` — same pattern as avalanchego, which would conflict.

**Decision needed:**
- Prefix tags (e.g., `firewood/v0.2.0`)
- Don't migrate tags — rely on the archived repo for historical references
- The `attach-static-libs.yaml` trigger needs updating if the tag scheme changes

### Docker / Container Builds

!! Container builds will need to be updated to use bazel eventually. But for now will just be using the existing published firewood go module.
Avalanchego's `Dockerfile` currently consumes firewood via pre-built static libraries
(the `firewood-go-ethhash/ffi` Go module includes `.a` files). No Rust toolchain is
needed in the container build.

If the build switches to compiling firewood from source (as Bazel does):
- The Dockerfile would need a Rust toolchain stage, increasing image size and build time
- Alternatively, continue using pre-built `.a` files for Docker/`go build` while Bazel
  compiles from source

**Decision needed:** What's the long-term container build strategy?

## Benchmarks & Performance Tracking

!! What the fuck is this section even for? incoherent
### Benchmark CI

- `benchmarks.yaml` — runs hashops and defer_persist benchmarks on main push (migrated
  by #5079)
- `track-performance.yml` — scheduled C-Chain reexecution benchmarks (weekday/weekly);
  deferred, blocked on benchmark data branch decision
- `bench-cchain-reexecution.sh` triggers avalanchego benchmark workflows cross-repo —
  references `ava-labs/firewood` paths and tokens that need updating
- `scripts/benchmark_cchain_range.sh` defines named firewood test presets
  (`firewood-101-250k`, `firewood-archive-101-250k`, `firewood-33m-33m500k`, etc.)
- The C-Chain reexecution benchmark JSON config matrices
  (`c-chain-reexecution-benchmark-container.json`,
  `c-chain-reexecution-benchmark-gh-native.json`) will need firewood entries added

### Publishing & GH Pages

Firewood publishes two things to GitHub Pages:
1. Rust API docs (`cargo doc` output) at `https://ava-labs.github.io/firewood/firewood`
2. Benchmark history/trends at `https://ava-labs.github.io/firewood/bench`

Benchmark data lives on a `benchmark-data` branch with append-only history, separating
main results (`bench/`) from feature branches (`dev/bench/{branch}/`).

!! The proposal is to publish to a new avalanchego location and add a 404 redirect to the old location
**Decision needed:**
- Continue publishing from archived firewood repo (docs go stale after migration)
- Publish from avalanchego under a subpath
- Set up redirects from old URLs
- Migrate the `benchmark-data` branch or redesign the workflow

!! Polyrepo is no longer needed post-migration
### Polyrepo / `FIREWOOD_REF` Workflow
The benchmark workflows and `scripts/run_polyrepo.sh` currently support
`FIREWOOD_REF=<commit>` for testing avalanchego against specific firewood commits
(e.g., `FIREWOOD_REF=abc123 task run-polyrepo`). Post-migration, firewood is in-tree
so this indirection is no longer needed.

- Remove `FIREWOOD_REF` support from `run_polyrepo.sh` and benchmark workflows
- Update the `--with-dependencies "firewood=abc123"` syntax in C-Chain reexecution
  benchmark workflows
- The polyrepo tool's `sync firewood@<ref>` command becomes unnecessary

## External Consumers & Import Paths

### Go Import Path Duality

Go source imports reference the published module path
`github.com/ava-labs/firewood-go-ethhash/ffi`. Bazel overrides resolution at build time
via gazelle directives, mapping the published path to the in-tree source.

This works for Bazel builds but:
- Go tooling outside Bazel (gopls, `go test`, IDE navigation) still sees the published
  path
- External consumers of avalanchego who `go get` it will pull from the published repo,
  not the subtree — version drift is possible
- If the published repo is archived, external consumers lose their dependency path

### Cross-Repository Interactions

Post-migration, these cross-repo workflows need tokens, permissions, and dispatch
targets updated:
- `attach-static-libs.yaml` pushes pre-built `.a` files to
  `ava-labs/firewood-go-ethhash` (uses `FIREWOOD_GO_GITHUB_TOKEN`)

!! This will become unnecessary, a refactored job will operate entirely within this repo
- `bench-cchain-reexecution.sh` triggers avalanchego benchmark workflows (uses
  `FIREWOOD_AVALANCHEGO_GITHUB_TOKEN` — may become unnecessary when benchmarks run
  in-repo)

## GitHub Administration

### Secrets

Firewood CI workflows require these secrets to be created in avalanchego:

- `CARGO_TOKEN` — crates.io publishing authentication
- `FIREWOOD_GO_GITHUB_TOKEN` — write access to `ava-labs/firewood-go-ethhash`
!! Unnecessary
- `FIREWOOD_AVALANCHEGO_GITHUB_TOKEN` — triggering benchmark workflows (may become
  unnecessary post-migration)
- AWS role assumption for chaos tests (S3 access)

!! These are already in the avalanchego repo. They don't have to be mentioned by name, it should be sufficient to note that they are present
The load/chaos test workflows also use monitoring secrets that likely already exist in
avalanchego but should be confirmed:
- `PROMETHEUS_URL`, `PROMETHEUS_PUSH_URL`, `PROMETHEUS_USERNAME`, `PROMETHEUS_PASSWORD`
- `LOKI_URL`, `LOKI_PUSH_URL`, `LOKI_USERNAME`, `LOKI_PASSWORD`

Repo secrets are write-only — the firewood team will need to provide values. Decide
whether to rotate to new credentials during migration or transfer existing values.

### Issue Migration

- **Decision needed:** Migrate all issues or just open ones?
- Bulk transfer is possible via `gh issue transfer` (supports batch via `gh` CLI)
- Transfer issue labels before issues — missing labels are silently dropped on transfer
- Firewood uses label categories that may not exist in avalanchego:
  - PR type labels (enforced by `label-pull-requests.yaml`)
  - Milestone associations
  - `dependencies`, `security`, `github-actions` (from dependabot)
!! Suggest keeping and prefixing so that firewood team can use their own
- Issue templates (bug report, feature request) — merge into avalanchego's templates or
  keep firewood-specific ones?
!! Why not check?
- Does firewood have a security disclosure process (SECURITY.md or similar) that needs
  migrating or merging with avalanchego's?

### Milestones

Milestones are repo-scoped in GitHub — firewood milestones will need to be recreated in
avalanchego (or namespaced, e.g. "Firewood v0.3").

!! Firewood milestones are separate
**Decision needed:** Do firewood milestones coexist with avalanchego milestones, or
merge into a unified scheme?

### Archival of Original Repo

After cutover:
- Archive `ava-labs/firewood`
- Add redirect notice in README pointing to `avalanchego/firewood/`
- Historical git tags, issues, and PRs remain accessible in archived state
- The `benchmark-data` branch with historical perf data should be preserved (migrate to
  avalanchego or keep in archived repo)

**Before cutover:** Agree on the archival plan so downstream consumers can be notified.

## Documentation Updates

!! These changes should arguably be made as part of the migration, no tafterwards
Post-migration, these docs need updating:
- `firewood/README.md` — repository URL, import paths, build instructions
- `firewood/CONTRIBUTING.md` — PR process, approval requirements, CI expectations
- `firewood/RELEASE.md` — publishing workflow from subdirectory
- `firewood/AGENTS.md` — workspace paths, build commands
- `docs/bazel.md` — already covers firewood FFI; may need expansion
- Root README — mention firewood as a component

## Items Requiring Verification

These claims are based on the firewood repo as of this writing and should be confirmed
against the actual grafted tree before cutover:

- **Workspace crate count:** 5 runtime crates (ffi, firewood, storage,
  firewood-macros, metrics) and 4 dev crates (replay, triehash, fwdctl, benchmark) —
  verify against `firewood/Cargo.toml` workspace members
- **Crates.io publication count:** "7 crates in topological order" — verify which
  workspace members are published
- **Cargo.lock size:** ~136K — verify actual size for impact on PR diffs
- **Rust version and edition:** Verify current MSRV and edition in `firewood/Cargo.toml`
  match what's configured in `MODULE.bazel`
