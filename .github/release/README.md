# GitHub release page automation

## Overview

Pushing a `v*.*.*` tag creates a **draft** GitHub release page with release notes
parsed from [`RELEASES.md`](../../RELEASES.md), a `Previous Tag:` pointer, and all
build artifacts attached ([#5162](https://github.com/ava-labs/avalanchego/issues/5162)).
A human reviews the draft and publishes it via the GitHub UI; nothing becomes
publicly visible without that step.

Audience: maintainers cutting releases; CI maintainers changing the release
pipeline or its artifact set.

## Usage

Cutting a release:

1. Ensure [`RELEASES.md`](../../RELEASES.md) has a section for the tag — either
   `## Pending (vX.Y.Z)` or `## [vX.Y.Z](url)` — with a non-empty body. The
   section must exist **in the tagged commit**; the workflow fails fast (before
   any build) if it doesn't.
2. Push the tag. The [`release` workflow](../workflows/release.yml) validates,
   fans out the five artifact builds, and creates the draft release.
3. Review the draft on GitHub (title, body, asset set, pre-release flag) and
   publish it. Decide "set as latest" in the UI — the workflow never sets it.

Tags with a semver suffix (e.g. `v1.15.0-rc1`) are marked `prerelease`.

Local validation (no GitHub mutation, no builds):

```sh
GH_REPO=ava-labs/avalanchego task release:dry-run -- v1.15.0
```

Every pipeline step is also exposed 1:1 — see `task --list-all | grep release:`
or [`Taskfile.yml`](./Taskfile.yml). The same scripts run in CI and locally.

Failure recovery:

- **"release already exists"** — the guard refuses to overwrite any existing
  release, draft included, and prints copy-paste commands to inspect and delete
  it by release ID. Delete, then re-push the tag.
- **"expected artifacts missing"** — a producer finished without uploading
  everything the manifest requires; the draft is not created.

Ad-hoc rebuilds of a single producer still work via each producer workflow's
`workflow_dispatch`. A commented-out direct-publish opt-in (skip the draft
step) lives in [`release.yml`](../workflows/release.yml).

## Conceptual model

```
push v*.*.* ── validate-tag ──┬─ build-rpms      ─┐
  classify stable/prerelease  ├─ build-linux     ─┤   publish
  duplicate-release guard     ├─ build-macos     ─┼─► body + publish-set gates
  RELEASES.md notes gate      ├─ build-deb-amd64 ─┤   draft release + post-assert
                              └─ build-deb-arm64 ─┘
```

One umbrella workflow ([`release.yml`](../workflows/release.yml)) owns the
`push: tags` trigger and calls the five producer workflows via `workflow_call`;
`needs:` makes `publish` wait for all of them, and artifacts are collected
in-run with `actions/download-artifact`. All logic lives in single-purpose
`release-*.sh` helpers under [`.github/workflows/`](../workflows/); the YAML
only wires them together.

[`release-expected-manifest.sh`](../workflows/release-expected-manifest.sh) is
the single source of truth for the asset set (20 basenames: 4 debs, 4 RPMs,
4 Linux tarballs + 4 `.sig`, 2 macOS zips + 2 `.sig`). The publish job copies
exactly the manifest entries into the publish set (failing on any missing one),
asserts set-equality before creating the release, and re-asserts against the
live release's assets afterwards.

## Maintenance notes

- **Adding/removing a release asset** requires two changes: the producer's
  `upload-artifact` step and the manifest. Both completeness gates enforce the
  manifest, so a drifted producer fails the run rather than shipping a partial
  release.
- **Drafts are invisible to tag-keyed APIs.** `GET /releases/tags/{tag}` (and
  `gh release view/delete <tag>`) excludes drafts, so every read/delete in this
  pipeline uses the listing endpoint filtered by `tag_name`. The same GitHub
  rule means draft listings require a push-capable token: `validate-tag` runs
  with `contents: write` solely so its duplicate-release guard can see drafts.
- **The existence guard fails closed.** It first proves the repo is reachable
  (`gh api repos/<repo>` must return 2xx) before trusting "no release found",
  and runs twice: in `validate-tag` and again immediately before release
  creation (the build matrix is a 30+ minute race window).
- **`$GITHUB_OUTPUT`/`$GITHUB_ENV` writes are assignment-first** (`v="$(helper)"`
  then `echo`): under `bash -e`, a failing command substitution inside `echo`'s
  arguments is silently swallowed, which would convert helper failures into
  empty outputs.
- **Deb basenames embed the codename** (`avalanchego-vX.Y.Z-{jammy,noble}-{arch}.deb`)
  because the deb producers emit identically-named files; the rename happens at
  publish-set assembly (by artifact-path substring), so S3 layout is unchanged.
- **The GPG public key is not a release asset.** It is distributed exclusively
  via S3; it rides inside the `rpms-*` artifacts for validation and the
  manifest-driven assembler ignores it.
- **`workflow-setup-packaging.sh` keeps its own tag resolution** instead of the
  shared `release-resolve-tag.sh`: it needs the `pull_request` fallback
  (`v0.0.0-pr.<sha>`), and on old-tag `workflow_dispatch` rebuilds the
  [packaging overlay](../packaging/README.md) restores only
  `.github/packaging/**`, so the shared resolver wouldn't exist on disk.
- **Prerequisites:** linux detached signatures
  ([#5160](https://github.com/ava-labs/avalanchego/issues/5160)) and macOS
  signing ([#5161](https://github.com/ava-labs/avalanchego/issues/5161)) must
  be merged before a tag can publish — the manifest lists 6 `.sig` companions
  and the pipeline fails closed without them.
- **Pre-merge testability:** `push: tags` reads workflow files from the tagged
  commit, so the pipeline can be exercised before merging by tagging a
  throwaway `v0.0.0-rc*` commit on a scratch branch (with a temporary
  `RELEASES.md` entry) and deleting the draft + tag afterwards.

## References

- [`release.yml`](../workflows/release.yml) — umbrella workflow
- [`release-*.sh` helpers](../workflows/) — one script per pipeline step; each
  has a usage header and is runnable from a developer shell
- [`Taskfile.yml`](./Taskfile.yml) — `task release:*` wrappers
- [`.github/packaging/README.md`](../packaging/README.md) — RPM/deb production
- [`softprops/action-gh-release`](https://github.com/softprops/action-gh-release) —
  release-creation action
- [`scripts/lib_version.sh`](../../scripts/lib_version.sh) — canonical
  `SEMVER_REGEX` shared with the tag-management scripts
