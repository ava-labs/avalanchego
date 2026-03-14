# Firewood CI Migration Changelog

This document records the adaptations made to firewood workflow files
when migrating them from the standalone `firewood` repo into the
`avalanchego` monorepo as a subtree at `firewood/`.

## Commit 1 — Pure renames (`git mv`)

All 14 workflows were moved with `git mv` to preserve rename tracking:

| Source | Target |
|---|---|
| `firewood/.github/workflows/ci.yaml` | `.github/workflows/firewood-ci.yml` |
| `firewood/.github/workflows/benchmarks.yaml` | `.github/workflows/firewood-benchmarks.yml` |
| `firewood/.github/workflows/ffi-nix.yaml` | `.github/workflows/firewood-ffi-nix.yml` |
| `firewood/.github/workflows/attach-static-libs.yaml` | `.github/workflows/firewood-attach-static-libs.yml` |
| `firewood/.github/workflows/expected-golangci-yaml-diff.yaml` | `.github/workflows/firewood-expected-golangci-yaml-diff.yml` |
| `firewood/.github/workflows/track-performance.yml` | `.github/workflows/firewood-track-performance.yml` |
| `firewood/.github/workflows/gh-pages.yaml` | `.github/workflows/firewood-gh-pages.yml` |
| `firewood/.github/workflows/release.yaml` | `.github/workflows/firewood-release.yml` |
| `firewood/.github/workflows/publish.yaml` | `.github/workflows/firewood-publish.yml` |
| `firewood/.github/workflows/pr-title.yaml` | `.github/workflows/firewood-pr-title.yml` |
| `firewood/.github/workflows/label-pull-requests.yaml` | `.github/workflows/firewood-label-pull-requests.yml` |
| `firewood/.github/workflows/metrics-check.yaml` | `.github/workflows/firewood-metrics-check.yml` |
| `firewood/.github/workflows/cache-cleanup.yaml` | `.github/workflows/firewood-cache-cleanup.yml` |
| `firewood/.github/workflows/default-branch-cache.yaml` | `.github/workflows/firewood-default-branch-cache.yml` |

## Commit 2 — Minimal adaptations

### Common adaptations applied to most workflows

| Adaptation | Reason |
|---|---|
| `push: branches: [main]` → `[master]` | avalanchego default branch is `master` |
| Add `paths:` filter (`firewood/**` + workflow self-reference) | Only trigger on firewood changes |
| Add `defaults: run: working-directory: firewood` | Cargo workspace lives at `firewood/` |
| Add `workspaces: "firewood -> target"` to `Swatinem/rust-cache` | Tell rust-cache where Cargo workspace is |
| Prefix artifact `path:` with `firewood/` | `upload-artifact` resolves paths from repo root |

### Per-workflow specific adaptations

| Workflow | Change | Reason |
|---|---|---|
| `firewood-ci.yml` | `name: ci` → `name: Firewood CI` | Disambiguate from avalanchego CI |
| `firewood-ci.yml` | `check-license-header` path/config prefixed with `firewood/` | Config files stayed in `firewood/.github/` |
| `firewood-ci.yml` | markdownlint globs prefixed with `firewood/` | Scope linting to firewood docs only |
| `firewood-ci.yml` | FFI job `working-directory: ffi` → `firewood/ffi` | FFI subdir moved with subtree |
| `firewood-ci.yml` | `go-version-file` / `cache-dependency-path` prefixed | Go module paths relative to repo root |
| `firewood-ci.yml` | Fuzz jobs: all paths prefixed with `firewood/` | Same as FFI — paths relative to repo root |
| `firewood-benchmarks.yml` | Artifact path `target/criterion` → `firewood/target/criterion` | Build output under firewood/ |
| `firewood-ffi-nix.yml` | PR path triggers prefixed with `firewood/` | Scope trigger to firewood FFI files |
| `firewood-attach-static-libs.yml` | Artifact path prefixed with `firewood/` | Build output under firewood/ |
| `firewood-attach-static-libs.yml` | Removed `path: firewood` from first checkout | Repo root already contains `firewood/` |
| `firewood-attach-static-libs.yml` | `main` → `master` in branch detection | Default branch difference |
| `firewood-expected-golangci-yaml-diff.yml` | PR paths prefixed, workflow refs updated | Paths changed with rename |
| `firewood-track-performance.yml` | `output-file-path` prefixed with `firewood/` | Results dir under firewood/ |
| `firewood-track-performance.yml` | `refs/heads/main` → `refs/heads/master` | Default branch difference |
| `firewood-gh-pages.yml` | No job-level defaults (mixed path contexts) | Build job runs cargo + git from different dirs |
| `firewood-gh-pages.yml` | `working-directory: firewood` on cargo doc step only | Surgical — only cargo needs firewood/ |
| `firewood-gh-pages.yml` | `target/doc/*` → `firewood/target/doc/*` | Copy paths relative to repo root |
| `firewood-gh-pages.yml` | Removed `rkuris/gh-pages` branch trigger | Branch-specific, not relevant in monorepo |
| `firewood-publish.yml` | `name: publish` → `name: firewood-publish` | Disambiguate |
| `firewood-release.yml` | `name: release` → `name: firewood-release` | Disambiguate |
| `firewood-pr-title.yml` | `name: pr-title` → `name: firewood-pr-title` | Disambiguate |
| `firewood-label-pull-requests.yml` | `name:` → `Firewood Label pull requests` | Disambiguate |
| `firewood-metrics-check.yml` | `name:` → `Firewood Metrics Change Check` | Disambiguate |
| `firewood-metrics-check.yml` | Scoped `git diff` and `grep` to `firewood/` | Only detect firewood metric changes |
| `firewood-metrics-check.yml` | Concurrency group prefixed with `firewood-` | Avoid collision with avalanchego workflows |
| `firewood-cache-cleanup.yml` | `name:` → `firewood cleanup caches by a branch` | Disambiguate |
| `firewood-default-branch-cache.yml` | `name:` → `firewood-default-branch-cache` | Disambiguate |

## Commit 3 — Avalanchego config updates

| File | Change | Reason |
|---|---|---|
| `.github/dependabot.yml` | Added `cargo` ecosystem for `/firewood` | Enable Rust dependency updates |
| `.github/CODEOWNERS` | Added `/firewood @ava-labs/firewood` | Assign ownership |
| `.github/workflows/ci.yml` | Added `paths-ignore: ["firewood/**"]` on `pull_request` | Don't trigger Go CI for Rust changes |
| `.github/workflows/coreth-ci.yml` | Added `paths-ignore: ["firewood/**"]` on `pull_request` | Same |
| `.github/workflows/subnet-evm-ci.yml` | Added `paths-ignore: ["firewood/**"]` on `pull_request` | Same |
| `.github/workflows/evm-ci.yml` | Added `paths-ignore: ["firewood/**"]` on `pull_request` | Same |
| `.github/workflows/codeql-analysis.yml` | Added `paths-ignore: ["firewood/**"]` on `pull_request` | Same |

## Non-workflow config files (kept in place)

These stay at `firewood/.github/` and are referenced using full paths:
- `firewood/.github/check-license-headers.yaml`
- `firewood/.github/license-header.txt`
- `firewood/.github/.golangci.yaml.patch`
- `firewood/.github/scripts/verify_golangci_yaml_changes.sh`

## Skipped files

GitHub only supports one set of these per repo:
- `firewood/.github/pull_request_template.md`
- `firewood/.github/ISSUE_TEMPLATE/*`
