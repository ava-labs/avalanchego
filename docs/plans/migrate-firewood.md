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
- [#5050](https://github.com/ava-labs/avalanchego/pull/5050) — Build firewood from
  source with Bazel. Hand-crafted BUILD.bazel files for the 5 crates in the runtime
  build graph (ffi, firewood, storage, firewood-macros, metrics). Also adds lint
  exclusions so avalanchego linters skip `firewood/`, a Rust toolchain version check
  (`scripts/check_rust_version.sh`), and will migrate the firewood load and chaos test
  jobs in a follow-up update.
- [#5079](https://github.com/ava-labs/avalanchego/pull/5079) — Migrate firewood CI
  workflows into the monorepo. Also adds `firewood/**` CODEOWNERS entries and Cargo
  entries to avalanchego's dependabot config.
- TBD — Refactor firewood's cross-repo benchmarking jobs (not yet started).

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

- Short-term: rubberstamping by avalanchego team members can satisfy the second approval
- Longer-term: CODEOWNERS scoping firewood team as required reviewers for `firewood/**`
  would let their approvals satisfy the requirement without rubberstamps
- **Decision needed:** Will the 2-approval requirement slow the firewood team down
  enough to warrant changing the policy?

### Breaking Changes Across the FFI Boundary

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

The firewood FFI has its own `go.mod` files (`firewood/ffi/go.mod`,
`firewood/ffi/tests/eth/go.mod`, `firewood/ffi/tests/firewood/go.mod`) that are
independent of avalanchego's module. #5050 uses `GOWORK=off` in CI to prevent Go
workspace mode from pulling these into the main build.

Developers running `go test` locally in firewood FFI directories need to be aware of
this — the root `go.work` file does not include these modules, and adding them would
break the main build.

### Development Tooling

Firewood uses tools not present in avalanchego's dev environment:
- `just` (justfile task runner) — 10KB of recipes for builds, tests, releases, benchmarks
- `git-cliff` — changelog generation from conventional commits
- `cargo-edit` — automated Cargo.toml version bumping
- `cbindgen` — C header generation (currently committed, not build-time)
- `cargo-nextest` — test runner used in CI
- `nix` flake for FFI builds (`ffi/flake.nix`)

**Decision needed:** Add these to avalanchego's dev environment, or phase them out in
favor of avalanchego equivalents (Taskfile, Bazel, etc.)?

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

**Decision needed:** Run full Rust CI from avalanchego workflows, or rely on Bazel for
what it covers and add targeted jobs for the rest?

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

The golangci-lint diff validation (`expected-golangci-yaml-diff.yaml`) is deferred —
its purpose is to ensure the Go lint config tracks expected deviations from an upstream
baseline, and it may need rethinking in the monorepo context.

### Rust Toolchain Synchronization

The Rust version and edition must match between `firewood/Cargo.toml` and
`MODULE.bazel`. A CI check (`scripts/check_rust_version.sh`, wired into
`task lint-all` via #5050) validates this.

- MSRV bumps in firewood require a coordinated `MODULE.bazel` update — easy to forget
- **Decision needed:** Can the firewood team bump MSRV independently, or does it
  require avalanchego team sign-off since it affects the Bazel toolchain?

### Supply Chain Security

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

Avalanchego's `Dockerfile` currently consumes firewood via pre-built static libraries
(the `firewood-go-ethhash/ffi` Go module includes `.a` files). No Rust toolchain is
needed in the container build.

If the build switches to compiling firewood from source (as Bazel does):
- The Dockerfile would need a Rust toolchain stage, increasing image size and build time
- Alternatively, continue using pre-built `.a` files for Docker/`go build` while Bazel
  compiles from source

**Decision needed:** What's the long-term container build strategy?

## Benchmarks & Performance Tracking

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

**Decision needed:**
- Continue publishing from archived firewood repo (docs go stale after migration)
- Publish from avalanchego under a subpath
- Set up redirects from old URLs
- Migrate the `benchmark-data` branch or redesign the workflow

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
- `bench-cchain-reexecution.sh` triggers avalanchego benchmark workflows (uses
  `FIREWOOD_AVALANCHEGO_GITHUB_TOKEN` — may become unnecessary when benchmarks run
  in-repo)

## GitHub Administration

### Secrets

Firewood CI workflows require these secrets to be created in avalanchego:

- `CARGO_TOKEN` — crates.io publishing authentication
- `FIREWOOD_GO_GITHUB_TOKEN` — write access to `ava-labs/firewood-go-ethhash`
- `FIREWOOD_AVALANCHEGO_GITHUB_TOKEN` — triggering benchmark workflows (may become
  unnecessary post-migration)
- AWS role assumption for chaos tests (S3 access)

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
- Issue templates (bug report, feature request) — merge into avalanchego's templates or
  keep firewood-specific ones?
- Does firewood have a security disclosure process (SECURITY.md or similar) that needs
  migrating or merging with avalanchego's?

### Milestones

Milestones are repo-scoped in GitHub — firewood milestones will need to be recreated in
avalanchego (or namespaced, e.g. "Firewood v0.3").

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
