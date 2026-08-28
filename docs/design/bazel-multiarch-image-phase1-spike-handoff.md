# Bazel multi-architecture image handoff

## Contents

- [Status](#status)
- [Current design](#current-design)
- [Cache findings](#cache-findings)
- [Builder-image cache decision](#builder-image-cache-decision)
- [Known warnings](#known-warnings)
- [Required next work](#required-next-work)
- [Validation](#validation)
- [Constraints](#constraints)

## Status

The Bazel validation path builds and tests an AvalancheGo OCI image for Linux
amd64 and Linux arm64. It is a validation path. It does not publish a production
image. The Dockerfile and Buildx path remain unchanged.

The Linux image CI job succeeds. It builds a locked Debian Bookworm builder,
builds both binaries, creates an OCI index, pushes that index to a local
registry, and runs both image platforms.

The current design and user guidance are in
[Bazel multi-architecture image validation](../bazel-multiarch-image-validation.md).
The broader design is in
[Bazel multi-architecture AvalancheGo image](./bazel-multiarch-image.md).

## Current design

The outer Bazel process runs on the GitHub Linux runner. It builds and loads an
amd64 Debian Bookworm builder image.

The inner Bazel process runs in that builder image. It builds AvalancheGo for
amd64 and arm64. The builder contains native GCC and the Debian aarch64 cross
compiler. The builder image digest is an action input. A result built with one
builder image cannot satisfy an action that uses another builder image.

`rules_distroless` supplies the locked Debian packages. The source override at
`a9e2012bf5935f7a8fa9c17a768abbbbd135f2a3` is required for
`apt.install(mergedusr = True)`. Without this setting, the package layer breaks
the Debian `/bin` symlink and the builder cannot run `/bin/sh`.

The BCR release does not yet contain this setting. Remove the override when a
BCR release includes it.

## Cache findings

GitHub Actions restores a repository cache and a Go module cache. These caches
contain downloaded inputs. They do not contain compiled AvalancheGo outputs.

The remote Bazel cache is configured on the runner. The inner builder uses a
different home directory. The validation script passes the remote-cache URL and
authorization header into the inner process when CI provides both values.

The first cache-fill job was
[run 33142777975](https://github.com/ava-labs/avalanchego/actions/runs/33142777975/job/98757322827?pr=5892).
It took 11 minutes and 45 seconds. Both direct binary builds had one remote
cache hit. The final image build ran 1,878 local actions.

The next unchanged job was
[run 33143521149](https://github.com/ava-labs/avalanchego/actions/runs/33143521149/job/98759771131?pr=5892).
It took 9 minutes and 9 seconds. The direct amd64 build had 939 remote hits out
of 950 actions. The direct arm64 build had 938 remote hits out of 949 actions.
The final image build still ran 1,878 local actions.

The final image command had `--remote_upload_local_results=false`. This setting
prevented the final image build from storing its binary actions. The direct
binary builds use different Bazel configurations. Their results cannot satisfy
the image target.

The pending change enables remote uploads for the final image command. It marks
only the OCI layer, manifest, index, push, and builder targets with
`no-remote-cache`. This lets Bazel cache the Debian-built binary actions while
it does not upload changing OCI outputs.

Do not assume that a cache hit for a direct binary build proves that the image
target can reuse that result. Check the action summary for the final image
command.

The CI bootstrap fetch and Task-bootstrap build both use `--test_env=HOME`.
This keeps their analysis configuration the same. Keep this option in
`scripts/run_task.sh` when changing the bootstrap commands.

## Builder-image cache decision

The builder image is a better cache candidate than the changing runtime image.
It changes only when the Debian lock, Bazel version, or builder definition
changes.

The local builder image is about 993 MB. The current remote-cache proxy rejects
its upload with HTTP 413, `Payload Too Large`. The builder build can still read
smaller remote action results. The large builder-image result cannot be stored
there.

Do not use the current Bazel remote cache for the complete builder image. The
current script disables its upload to prevent an HTTP 413 warning on every job.

GitHub Container Registry (GHCR) is the candidate cache for the builder image.
Use a separate package, such as
`ghcr.io/ava-labs/avalanchego-bazel-builder`. Use an immutable tag derived from
all builder inputs:

- `.bazelversion`;
- `MODULE.bazel.lock`;
- `bazel/image/bookworm.lock.json`; and
- the builder-image BUILD files.

A CI job should pull this tag first. It should build and push the image only on
a cache miss. The job must pass the pulled image digest into the inner Bazel
action environment.

Use `GITHUB_TOKEN` with `packages: write` only for trusted same-repository
jobs. Fork pull requests must not push package versions. They can pull a public
or authorized cache entry, or build the builder locally.

Confirm GHCR retention and cleanup rules before implementation. The desired
policy is a short retention period, such as 24 or 48 hours. Do not use GHCR for
the changing runtime test image.

The Debian base image is pinned by digest. `rules_img` pulls its base layers
shallowly. The repository cache already preserves package inputs. A GHCR
builder image would also preserve the assembled toolchain image.

## Known warnings

The image job has these warnings:

- `rules_distroless` reports unresolved arm64 cross-library symlinks and linker
  script paths. The builder works because the flattened Debian package tree
  contains the files. This is an upstream package-metadata warning. Do not
  suppress it without an upstream fix or a test that proves the package model
  is complete.
- `rules_img` reports that the GitHub Docker daemon does not expose its
  containerd store. It falls back to `docker load`. This runner limitation also
  changes the locally loaded image digest. Do not treat that digest as the OCI
  builder digest used for action keys.

## Required next work

1. Finish and commit the pending remote-cache changes.
2. Run the image job once to fill the final image target cache.
3. Run the job again without source changes.
4. Check that the final image command reports remote cache hits for binary
   actions. It may still run OCI layer, manifest, index, and push actions.
5. Measure the total job time after the second run.
6. Decide whether the measured builder build cost justifies a GHCR builder
   cache. The current builder build takes about 30 seconds. Pulling and loading
   a 993 MB image can cost more.
7. If GHCR is justified, design the package name, immutable tag input hash,
   trusted-write rule, fork fallback, and scheduled cleanup before adding it.
8. Investigate the `rules_distroless` warnings upstream. Do not add a local
   suppression that can hide missing cross-library files.

## Validation

Run these checks after changes to the validation script, image rules, or CI
configuration:

```text
task bazel-build-image
task bazel-test-build-image
bash -n scripts/bazel_image_spike.sh
task lint-shell
task lint-action
git diff --check
```

For cache changes, run one cache-fill CI job and one unchanged CI job. Record
the action summaries for the amd64 binary, arm64 binary, and final image build.

## Constraints

- Keep the Dockerfile and Buildx path unchanged.
- Keep the Debian 12 slim runtime base and dynamic glibc linking.
- Support Linux amd64 and Linux arm64 only.
- Keep the outer and inner Bazel cache roots separate.
- Do not put remote-cache or registry credentials in the builder image,
  workspace, or command output.
- Do not treat the local validation registry as a production publishing design.
