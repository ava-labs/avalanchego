# macOS zip packaging

## Overview

This document describes how AvalancheGo and Subnet-EVM binaries are packaged
for macOS distribution: Apple code-signed and notarized Mach-O binaries
delivered as detached-GPG-signed `*.zip` archives.

It exists so future maintainers do not need to reconstruct the macOS signing
and notarization pipeline from CI YAML, shell scripts, and the
`indygreg/apple-platform-rs` source tree alone.

This document is for two audiences:

- **users/release engineers** who need to build, validate, or publish macOS
  zip releases
- **maintainers** who need to change the macOS packaging implementation safely

## Usage

### What gets produced

Per architecture, the macOS packaging pipeline produces:

| Artifact | Contents |
| --- | --- |
| `avalanchego-macos-<TAG>.zip` | Apple-signed `build/avalanchego` Mach-O binary |
| `avalanchego-macos-<TAG>.zip.sig` | detached GPG signature over the avalanchego zip |
| `subnet-evm-macos-<TAG>.zip` | Apple-signed `build/subnet-evm` Mach-O binary |
| `subnet-evm-macos-<TAG>.zip.sig` | detached GPG signature over the subnet-evm zip |

Each Mach-O binary inside a zip carries an Apple code signature with the
`runtime` hardened-runtime flag. Each zip is signed with the project's GPG
key and submitted to Apple's notary service before being published. The
order is load-bearing; see *Apple-sign / GPG-sign / notarize order* in the
maintenance notes.

### Main entrypoints

- CI workflow: `.github/workflows/build-macos-release.yml`
- Production zip + GPG-sign script: `.github/workflows/build-zip-pkg.sh`
- Packaging Taskfile: `.github/packaging/Taskfile.yml`
- Local validator build script: `.github/packaging/scripts/build-macos-zip.sh`
- Local validator script: `.github/packaging/scripts/validate-macos-zip.sh`
- Local builder Docker image: `.github/packaging/Dockerfile.macos-zip`
- Builder-image bootstrap: `.github/packaging/scripts/build-macos-zip-builder-image.sh`

### Local build and validation

Build and validate the full macOS zip pipeline locally with:

```bash
task packaging:test-build-macos-zip
```

This runs in a `debian:stable-slim`-based Docker container that mirrors the
toolchain of the CI sign-publish job (`p7zip-full`, `gnupg`, `openssl`, `jq`,
`rcodesign`). It exercises the production `build-zip-pkg.sh` unmodified
against cross-compiled stub Mach-O binaries, then validates the produced zips
in a fresh container.

Useful environment variables:

- `PACKAGING_TAG` - tag/version label to embed, defaults to `v0.0.0`
- `GPG_KEY_FILE` - real GPG signing key for non-ephemeral signing
- `GPG_KEY_PASSPHRASE` - passphrase for the GPG signing key
- `MACOS_SIGNING_PKCS12_BASE64` - base64-encoded Apple signing PKCS#12 for
  non-ephemeral Apple signing
- `MACOS_SIGNING_PASSWORD` - password for the PKCS#12
- `MACOS_NOTARIZATION_AUTH_KEY` - base64-encoded App Store Connect API
  private key (ECDSA P8)
- `MACOS_NOTARIZATION_KEY_ID`, `MACOS_NOTARIZATION_ISSUER_ID` - identifiers
  for the API key

Without `GPG_KEY_FILE`, the build generates a short-lived ephemeral GPG key.
Without `MACOS_SIGNING_PKCS12_BASE64`, it generates a throwaway self-signed
PKCS#12 via `rcodesign generate-self-signed-certificate`. Without
`MACOS_NOTARIZATION_AUTH_KEY`, it generates a throwaway ECDSA P8 key via
`openssl`. Local builds therefore always exercise the signing and
key-materialization paths, regardless of secret availability.

### CI behavior

`.github/workflows/build-macos-release.yml` triggers on:

- **tag push** (`push: tags: ["*"]`) - builds the macOS release for the
  pushed tag
- **workflow_dispatch** - builds for an explicitly provided tag

The workflow runs two jobs:

1. `build-mac` (macos-14): builds the Mach-O binaries natively, uploads them
   as the `macos-binaries` workflow artifact.
2. `sign-publish` (ubuntu-24.04): downloads `macos-binaries`, materializes
   Apple credentials, Apple-signs the binaries, runs the production
   `build-zip-pkg.sh` to zip and GPG-sign, submits each zip to Apple's notary
   service, then publishes `*.zip` and `*.zip.sig` to
   `s3://${secrets.BUCKET}/macos/` and as GitHub workflow artifacts.

The workflow imports `secrets.RPM_GPG_PRIVATE_KEY` for GPG signing and the
sec-team-issued `secrets.MACOS_SIGNING_*` and `secrets.MACOS_NOTARIZATION_*`
for Apple signing and notarization. An empty `RPM_GPG_PRIVATE_KEY` fails the
workflow at the `Import GPG key` step (see *Fail-fast on missing GPG secret*
below).

## Conceptual model

The macOS pipeline intentionally splits the build and sign-publish stages
across two GitHub runner images rather than doing everything on a macOS
runner.

### Two-job split: `build-mac` and `sign-publish`

| Job | Runner | Purpose |
| --- | --- | --- |
| `build-mac` | `macos-14` | native Mach-O build only |
| `sign-publish` | `ubuntu-24.04` | Apple sign + GPG sign + notarize + upload |

Apple signing, notarization, and publishing all run on Linux because:

- `indygreg/apple-code-sign-action@v1` is a Node JS action and runs on any
  runner that supports Node 20+
- the underlying `rcodesign` tool is cross-platform and ships pre-built Linux
  binaries for both `x86_64` and `aarch64`
- macOS GitHub-hosted minutes are roughly 10× the price of Linux minutes; the
  parts of the pipeline that do not require a Mach-O build environment should
  not pay that premium

### Credential lifecycle

All Apple credentials and the GPG key are materialized into `mktemp` files
and are explicitly removed by the workflow's `Cleanup` step (which runs
`if: always()`). They are not echoed via `GITHUB_OUTPUT` and never reach
either the GitHub Releases page or the `s3://${BUCKET}/macos/` prefix.

The local validator mirrors this: ephemeral Apple PKCS#12, GPG private key,
and ECDSA P8 are all written into a `mktemp -d` stage directory cleaned up by
an `EXIT` trap. In particular, the encoded App Store Connect API key JSON
(which contains the private key) is **never** copied to the bind-mounted
`OUTPUT_DIR`; its shape is parse-checked inline before the stage directory
is removed.

### Artifact path arithmetic

`actions/upload-artifact@v4+` strips the least-common-ancestor directory
prefix when given multiple paths. So the upload step:

```yaml
- uses: actions/upload-artifact@v6
  with:
    name: macos-binaries
    path: |
      build/avalanchego
      build/subnet-evm
```

produces an artifact whose internal entries are `avalanchego` and `subnet-evm`
(without the `build/` prefix). The download step must restore that prefix:

```yaml
- uses: actions/download-artifact@v6
  with:
    name: macos-binaries
    path: build
```

Downstream steps reference `build/avalanchego` and `build/subnet-evm`
explicitly, so the `path: build` is load-bearing - using `path: .` would
land the files at the workspace root and break the rest of the workflow.

The production `build-zip-pkg.sh` then calls `7z a foo.zip build/avalanchego`,
which preserves the `build/` prefix inside the zip. The validator therefore
expects `build/<pkg>` as the internal entry when listing or extracting.

### Same pipeline for local and CI

The local validator (`build-macos-zip.sh`) invokes the production
`.github/workflows/build-zip-pkg.sh` script unmodified. Local validation
therefore exercises the same zip + GPG-sign code that runs in CI, not a copy.
Behavioral divergence between local and CI is structurally avoided.

The Apple-sign and notarize steps are not yet wrapped this way because they
live inside the `indygreg/apple-code-sign-action@v1` Node JS action rather
than in a separate script; the local validator invokes `rcodesign` directly
with the same flag shapes the action uses.

## Maintenance notes

### Why `rcodesign` rather than Apple's `codesign`

Apple's `codesign` only runs on macOS. The sign-publish job runs on Linux for
cost reasons (see *Two-job split* above), so the signing tool must work on
Linux. `rcodesign` (from `indygreg/apple-platform-rs`) is a pure-Rust
re-implementation of Apple code-signing and notary submission that works
without macOS. It is the same tool wrapped by
`indygreg/apple-code-sign-action`, so using `rcodesign` directly in the local
validator and via the action in CI keeps the underlying signing
implementation consistent across both paths.

### Why `indygreg/apple-code-sign-action@v1` is currently pinned by major tag

The action is currently pinned to the floating `v1` tag rather than a full
commit SHA. The pragmatic trade-off:

- `v1` follows the same pinning pattern as other GitHub Actions in this
  repository's release workflows (`actions/checkout@v5`,
  `aws-actions/configure-aws-credentials@v6`, etc.). Internal consistency.
- A retag of `v1` to a malicious commit would let an attacker exfiltrate the
  PKCS#12 password and notarization API key. The action is on a personal
  GitHub namespace (`indygreg/`), which is meaningfully higher supply-chain
  risk than first-party `actions/*` and `aws-actions/*` pins.

The tarball that `indygreg/apple-platform-rs` ships is already SHA256-pinned
in the local validator's Dockerfile (`Dockerfile.macos-zip`), via the
`*.sha256` sidecar from the release. The CI workflow itself does not yet
SHA-pin either the action or the rcodesign tarball it downloads inline.

When the upstream API stabilizes or supply-chain risk increases enough to
justify the maintenance overhead, pin to a full commit SHA and enable
Dependabot to update it.

### Apple-sign / GPG-sign / notarize order

The CI workflow performs the signing operations in this order, and the
order is load-bearing:

1. **Apple-sign each Mach-O binary** (`rcodesign sign` via the action). The
   signature is embedded inside the Mach-O via the `LC_CODE_SIGNATURE` load
   command.
2. **Zip each binary and detached-GPG-sign the resulting zip**
   (`build-zip-pkg.sh`). The GPG signature is over the zip bytes, which
   include the already-Apple-signed Mach-O.
3. **Submit each zip to Apple's notary service** (`rcodesign
   notary-submit`). Apple's notary inspects the zip and approves it server-
   side. For zip containers, the notarization ticket is not stapled into the
   zip itself; the zip bytes are unchanged after notarization.

Re-ordering would break the GPG signature: if notarization were to run
before GPG-signing, any future Apple-side rewrite of the zip would silently
invalidate the GPG signature; if zipping ran before Apple-signing, the
Mach-O inside the zip would be unsigned.

### Fail-fast on missing GPG secret

The `Import GPG key` step asserts that `RPM_GPG_PRIVATE_KEY` is non-empty
and that the materialized key file is non-zero-size before exporting
`GPG_KEY_FILE` to `$GITHUB_ENV`.

This is a deliberate guard against a previously-latent failure mode:

- `printf '%s' "${RPM_GPG_PRIVATE_KEY}" > "${GPG_KEY_FILE}"` produces a
  zero-byte file when the secret is empty.
- `build-zip-pkg.sh` tests `[[ -s "${GPG_KEY_FILE}" ]]` and silently takes
  the no-op signing branch on a zero-byte file (this branch is intentional;
  it lets the script be reused in contexts that do not have a key).
- The subsequent `Upload zips to S3` step in the workflow unconditionally
  copies `*.zip` and then `*.zip.sig`. Without the precondition, `*.zip`
  is published successfully and then the upload fails on the missing
  `*.zip.sig`, leaving an unsigned zip in the release bucket.

The precondition replaces "publish unsigned then error" with "fail at the
import step with a clear message."

### Why `secrets.RPM_GPG_PRIVATE_KEY` is shared with RPM signing

The macOS zip workflow reuses the same GPG private key as the RPM packaging
workflow rather than introducing a `MACOS_GPG_PRIVATE_KEY` secret.

Rationale: the GPG key identifies *the project*, not the package format. End
users who want to verify "is this artifact really from ava-labs?" use the
same key whether they downloaded an RPM or a macOS zip (Linux tarballs are
currently unsigned; if they ever gain GPG signing, the same key would
naturally be reused). Maintaining two keys for the same provenance claim
would create rotation and trust-chain complexity without a corresponding
security benefit. The `RPM_GPG_PRIVATE_KEY` secret name predates macOS zip
signing; renaming it would require coordinated changes across multiple
workflows. The name is historical; the key is project-wide.

If the project ever needs per-format signing keys (for example, to support
different rotation cadences), the migration path is: add new format-specific
secrets, update the workflows in lockstep, then remove `RPM_GPG_PRIVATE_KEY`.

### `MACOS_SIGNING_*` and `MACOS_NOTARIZATION_*` are separate secret families

Apple credentials are split into two families by the security team:

| Family | Secrets | Purpose |
| --- | --- | --- |
| `MACOS_SIGNING_*` | `PKCS12_BASE64`, `PASSWORD` | PKCS#12 holding the Developer ID cert + private key used to sign binaries |
| `MACOS_NOTARIZATION_*` | `AUTH_KEY`, `KEY_ID`, `ISSUER_ID` | App Store Connect API key used to submit binaries to Apple's notary service |

The split reflects two different credential lifecycles:

- The signing certificate is issued by Apple's Developer Program, lives in an
  HSM/keyvault, and rotates on Apple's certificate-expiry schedule (typically
  multi-year). Compromise of this certificate would let an attacker sign
  software as the project.
- The App Store Connect API key is project-managed, rotatable on the
  project's own schedule, and only grants access to submit to Apple's
  notarization service. Compromise of this key alone does not enable signing.

Workflow-internal env vars (`APPLE_P12_FILE_B64`, `APPLE_API_KEY_B64`, etc.)
intentionally use shorter, format-agnostic names so the credential-
materialization logic stays readable. The `secrets.*` references at the
boundary use the sec-team naming convention.

### Local validator scope

The local validator is intentionally "Tier 1": it exercises the parts of the
pipeline that are reproducible offline. What it does and does not cover:

| Part | Local validator | Why |
| --- | --- | --- |
| Apple-signing a Mach-O | yes, with `rcodesign generate-self-signed-certificate` | rcodesign does not check the trust chain at sign-time, so a self-signed cert exercises the same code path |
| GPG-signing a zip + verifying | yes, with ephemeral RSA key | full roundtrip in `gpg --import` + `gpg --verify` |
| `rcodesign encode-app-store-connect-api-key` | yes, with `openssl`-generated ECDSA P8 | encode step is purely local, no network |
| `rcodesign notary-submit` (notarization) | no | hardcoded `https://appstoreconnect.apple.com/notary/v2/submissions` endpoint in `app-store-connect/src/notary_api.rs`; no `--dry-run`, no env-var endpoint override |

Reaching Tier 2 (running the indygreg action via `act`) or Tier 3 (mocking
the notary HTTPS endpoint) would require more infrastructure than the
current Tier-1 setup. Tier 1 covers ~80% of the value at ~5% of the
complexity: any divergence between local and CI for the signing/zip/GPG
path will surface; only Apple's notary submission itself is untested
locally.

### Stub Mach-O binaries in the local validator

The local validator does not depend on the real `avalanchego` or
`subnet-evm` binaries being present. It cross-compiles a trivial three-line
Go program (`package main; func main() {}`) twice with `GOOS=darwin
GOARCH=arm64 CGO_ENABLED=0 go build` to produce real Mach-O binaries.

The validator's purpose is to verify the *packaging* path - sign, zip,
GPG-sign, encode, validate - not to test the real avalanchego binaries.
Stub binaries make the test reproducible from any host (Linux or macOS),
fast, and independent of upstream build state. Real binaries are signed
and notarized only in the CI workflow on a tag push.

### Architecture naming

Three architecture naming schemes are in play and must stay aligned:

| Context | x86 64-bit | ARM 64-bit |
| --- | --- | --- |
| `uname -m` | `x86_64` | `arm64` |
| Go downloads | `amd64` | `arm64` |
| rcodesign release assets | `x86_64-unknown-linux-musl` | `aarch64-unknown-linux-musl` |

`scripts/build-macos-zip-builder-image.sh` normalizes between these
conventions when fetching Go and rcodesign tarballs from upstream. The
Dockerfile receives the normalized arch via `TARGETARCH` and re-derives the
upstream-asset names from it. The mappings live in one place per file
because each upstream uses its own convention.

### Invariants to preserve

If you change this area, preserve these unless you are intentionally
revisiting the design:

- the local validator invokes the production `build-zip-pkg.sh` script
  unmodified, not a copy
- `secrets.RPM_GPG_PRIVATE_KEY` is the GPG signing key for both RPM and
  macOS zip artifacts
- `actions/download-artifact@v6` uses `path: build` (not `.`) so downstream
  references to `build/<pkg>` work
- Apple-sign runs before GPG-sign, which runs before notarize
- ephemeral credentials never leak from the stage directory to the bind-
  mounted `OUTPUT_DIR` or the published artifacts
- `Import GPG key` fails fast on an empty `RPM_GPG_PRIVATE_KEY` rather than
  letting the no-op signing branch produce an unsigned release
- the macOS Mach-O binaries are built natively on `macos-14`; all other
  operations run on `ubuntu-24.04`

### Revisit this design if

The current design should be reconsidered if any of these change:

- rcodesign gains a `--dry-run` flag or env-var endpoint override for
  notarization (would enable a Tier 3 local mock)
- Apple migrates away from an rcodesign-compatible API (would force a fall-
  back to Apple's `notarytool` or `codesign`, which only run on macOS)
- the security team rotates to a per-format GPG signing key (would require
  splitting `RPM_GPG_PRIVATE_KEY` into format-specific secrets)
- GPG signing moves to KMS / cloud-managed keys (would replace
  `setup_gpg`'s key-import branch with a remote-signing branch)
- macOS GitHub-hosted minutes become cost-competitive with Linux (would
  collapse the two-job split back into one)
- the project moves zip publishing off S3 / GitHub Releases to another
  distribution channel

## References

### Repository entrypoints

- Workflow: `/.github/workflows/build-macos-release.yml`
- Production zip + GPG-sign script: `/.github/workflows/build-zip-pkg.sh`
- Packaging Taskfile: `/.github/packaging/Taskfile.yml`
- Builder image: `/.github/packaging/Dockerfile.macos-zip`
- Builder-image bootstrap: `/.github/packaging/scripts/build-macos-zip-builder-image.sh`
- Local validator build: `/.github/packaging/scripts/build-macos-zip.sh`
- Local validator: `/.github/packaging/scripts/validate-macos-zip.sh`
- Shared packaging helpers: `/.github/packaging/scripts/lib-build-common.sh`
- Root task include: `/Taskfile.yml`

### Upstream tooling

- [`indygreg/apple-code-sign-action`](https://github.com/indygreg/apple-code-sign-action) - GitHub Action wrapping rcodesign
- [`indygreg/apple-platform-rs`](https://github.com/indygreg/apple-platform-rs) - rcodesign source; relevant code paths: `apple-codesign/src/cli/mod.rs` (CLI surface), `app-store-connect/src/notary_api.rs` (hardcoded notary endpoint)
- [`rcodesign` documentation](https://gregoryszorc.com/docs/apple-codesign/stable/) - including [certificate management](https://gregoryszorc.com/docs/apple-codesign/stable/apple_codesign_certificate_management.html) and [signing reference](https://gregoryszorc.com/docs/apple-codesign/stable/apple_codesign_rcodesign_signing.html)
- [Apple notary service overview](https://developer.apple.com/documentation/security/customizing_the_notarization_workflow)
