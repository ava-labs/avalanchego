# Packaging

This directory contains the packaging implementations for AvalancheGo and
Subnet-EVM release artifacts. Each supported distribution format has its own
build and validation pipeline documented separately:

- [**RPM**](rpm.md) - signed RPMs for RHEL 9.x-compatible Linux systems
  (`x86_64`, `aarch64`).
- [**DEB**](deb.md) - signed DEBs for Ubuntu 22.04 (jammy) and 24.04 (noble)
  (`amd64`, `arm64`).
- [**macOS zip**](macos-zip.md) - Apple-signed, notarized, and GPG-signed
  `*.zip` archives for macOS.

This README is an index. The per-format documents are the source of truth
for what each pipeline does, how to use it, and why it is implemented the way
it is.

## What is shared between formats

The pipelines deliberately share infrastructure where it makes sense to keep
the local-and-CI invariants aligned:

- **Project GPG signing key** - `secrets.RPM_GPG_PRIVATE_KEY` signs RPM, DEB,
  and macOS zip artifacts. The secret name predates DEB and macOS zip signing
  but the key identifies the project, not the package format. See the macOS zip
  doc for the rotation-path notes.
- **Unified Linux workflow** - `.github/workflows/build-linux-packages.yml`
  builds both RPM and DEB from a single format × arch matrix that delegates bulk
  work to the `.github/packaging/actions/build-package` composite action. macOS
  zips are built by the separate `.github/workflows/build-macos-release.yml`.
- **Taskfile surface** - `task packaging:<target>` is the entrypoint for both
  local builds and local validation. The root `Taskfile.yml` includes this
  directory's `Taskfile.yml`.
- **Helper library** - `scripts/lib-build-common.sh` provides `setup_gpg`,
  `assert_files_exist`, `use_ephemeral_gpg_passphrase`, and friends. All three
  format pipelines source it.
- **"Single pipeline for local and CI" invariant** - the local validators
  invoke the same production build scripts the CI workflows run. There is
  intentionally no separate "local" code path that could diverge.

## Layout

```
.github/packaging/
├── README.md              this index
├── rpm.md                 RPM pipeline documentation
├── deb.md                 DEB pipeline documentation
├── macos-zip.md           macOS zip pipeline documentation
├── Taskfile.yml           shared Taskfile (RPM + DEB + macOS zip tasks)
├── Dockerfile.rpm         RPM builder image (Rocky Linux 9)
├── Dockerfile.deb         DEB builder image (Ubuntu 22.04)
├── Dockerfile.macos-zip   macOS zip validator image (Debian)
├── actions/
│   └── build-package/     composite action shared by the Linux workflow
├── nfpm/                  nfpm package definitions (RPM + DEB)
└── scripts/
    ├── lib-build-common.sh
    ├── build-builder-image.sh
    ├── build-macos-zip-builder-image.sh
    ├── build-package.sh
    ├── build-macos-zip.sh
    ├── validate-rpm.sh
    ├── validate-deb.sh
    ├── validate-macos-zip.sh
    ├── smoke-test.sh
    └── workflow-setup-packaging.sh
```

## Adding a new format

When adding a fourth format (for example a Linux tarball / `apk` / Windows
archive), the pattern to mirror is:

1. Add a per-format `Dockerfile.<format>` if a build container is needed.
2. Add a per-format `<format>.md` document following the four-layer
   structure described in `docs/documentation-guidelines.md`. Use any of
   `rpm.md`, `deb.md`, or `macos-zip.md` as a model.
3. Add a per-format build script and validator under `scripts/` (reuse
   `build-package.sh` if the format fits the shared nfpm path).
4. Add per-format Taskfile entries (`build-<format>`, `validate-<format>`,
   `test-build-<format>`).
5. Link the new doc from the list at the top of this README.

The shared `setup_gpg`, `assert_files_exist`, and Taskfile structure should
not need to change for a new format that fits the same "build → sign →
validate-in-fresh-container" shape.
