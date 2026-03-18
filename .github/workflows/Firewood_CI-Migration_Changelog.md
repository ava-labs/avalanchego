# Firewood CI Migration — Decision Log

## Workflow Disposition

| Source | Target | Status | Notes |
|--------|--------|--------|-------|
| `ci.yaml` | `firewood-ci.yml` | Migrated | Main CI |
| `benchmarks.yaml` | `firewood-benchmarks.yml` | Migrated | Push-to-master benchmarks |
| `attach-static-libs.yaml` | `firewood-attach-static-libs.yml` | Migrated | Multi-arch static lib builds |
| `track-performance.yml` | `firewood-track-performance.yml` | Migrated | C-Chain reexecution perf |
| `release.yaml` | `firewood-release.yml` | Migrated | Draft release on tags |
| `publish.yaml` | `firewood-publish.yml` | Migrated | crates.io publishing |
| `label-pull-requests.yaml` | `firewood-label-pull-requests.yml` | Migrated | PR labeling |
| `metrics-check.yaml` | `firewood-metrics-check.yml` | Migrated | Metrics change detection |
| `cache-cleanup.yaml` | `firewood-cache-cleanup.yml` | Migrated | PR cache cleanup |
| `default-branch-cache.yaml` | `firewood-default-branch-cache.yml` | Migrated | Default branch cache warm |
| `expected-golangci-yaml-diff.yaml` | — | Removed | Same repo; FFI lints against `.golangci.yml` directly |
| `ffi-nix.yaml` | — | Removed | Bazel replaces Nix for FFI builds |
| `gh-pages.yaml` | — | Deferred | GitHub Pages URL tied to originating repo |
| `pr-title.yaml` | — | Deferred | Release-note scoping needs investigation |

## `firewood/.github/` File Disposition

| File | Disposition | Reason |
|------|-------------|--------|
| `check-license-headers.yaml` | Kept | Referenced by `firewood-ci.yml` |
| `license-header.txt` | Kept | Referenced by `firewood-ci.yml` |
| `.golangci.yaml.patch` | Removed | Patch mechanism obsolete |
| `.gitignore` | Removed | Only gitignored verify script artifacts |
| `scripts/verify_golangci_yaml_changes.sh` | Removed | Patch mechanism obsolete |
| `dependabot.yml` | Merged into root | Config merged into `.github/dependabot.yml` |
| `pull_request_template.md` | Removed | GitHub supports one per repo |
| `ISSUE_TEMPLATE/*` | Removed | GitHub supports one set per repo |

## Common Adaptations (applied to all 10 migrated workflows)

- **working-directory**: Added `defaults: run: working-directory: firewood` (Cargo workspace lives at `firewood/`)
- **Path filters**: Added `paths:` trigger scoped to `firewood/**` + workflow self-reference
- **Branch refs**: `push: branches: [main]` → `[master]` (avalanchego default branch)
- **rust-cache**: Added `workspaces: "firewood -> target"` to `Swatinem/rust-cache`
- **Artifact paths**: Prefixed `upload-artifact` paths with `firewood/`
- **GOWORK=off**: Set on FFI and fuzz jobs to isolate firewood Go modules from monorepo workspace

## Post-migration Fixes

| File | Change | Reason |
|------|--------|--------|
| `bazel-ci.yml`, `c-chain-reexecution-benchmark-container.yml`, `c-chain-reexecution-benchmark-gh-native.yml` | Added `paths-ignore: ["firewood/**"]` | Don't trigger avalanchego-only workflows for firewood changes |
| `firewood/.github/check-license-headers.yaml` | Exempted `BUILD.bazel` files | Bazel build files don't carry firewood license headers |
| `firewood-cache-cleanup.yml`, `firewood-metrics-check.yml` | Fixed shellcheck warnings | Lint compliance with avalanchego's CI |
| `firewood-track-performance.yml` | Suppressed shellcheck SC2129 | Lint compliance |
| `firewood/ffi/go.mod` | Removed stale dependencies | Tidied go.mod after monorepo integration |
| `firewood-attach-static-libs.yml` | `ubuntu-22.04-arm` → `custom-arm64-jammy` | Use avalanchego's registered ARM runner label |
| `firewood-attach-static-libs.yml` | Pass `github.event.pull_request.head.ref` through env var | Fix actionlint script injection warning |
| `firewood-track-performance.yml` | Removed `timeout-minutes` from `workflow_dispatch` inputs | GitHub Actions limits `workflow_dispatch` to 10 inputs; default 12h for manual dispatch |
| `scripts/actionlint.sh` | Skip `firewood-*` workflows in `run_task.sh` enforcement check | Firewood workflows use their own script conventions |

## Known Issues

| Workflow | Issue | Resolution |
|----------|-------|------------|
| `firewood-attach-static-libs.yml` | `push-firewood-ffi-libs`, `test-firewood-ffi-libs`, `remove-if-pr-only` jobs skipped | `FIREWOOD_GO_GITHUB_TOKEN` secret not configured in avalanchego repo; requires repo-admin setup |
