# GitHub Release Process Automation Plan

**Goal:** Given a tag, produce a complete GitHub release with all signed artifacts — fully automated.

**Epic:** [#5157 — Automate release process](https://github.com/ava-labs/avalanchego/issues/5157)

**Architecture:** A unified GitHub Actions workflow orchestrates platform-specific build jobs, signs all artifacts (GPG via repo secrets for Linux, codesign+notarytool for macOS), uploads packages to S3, and creates the GitHub release with all assets attached. KMS-backed signing is a future hardening step after secrets-based signing is proven.

---

## Current State

The release process today is semi-automated. Tag push triggers independent build workflows, but signing, artifact collection, and release page creation require manual intervention.

### What exists

* **Linux binary tarballs** — built by [`build-linux-binaries.yml`](.github/workflows/build-linux-binaries.yml)
  * amd64 and arm64, uploaded to S3
  * GPG signing is **manual** (done offline via `create-github-release.sh`)
* **macOS binaries** — built by [`build-macos-release.yml`](.github/workflows/build-macos-release.yml)
  * Uploaded to S3
  * codesign + notarization is **manual**
* **DEB packages** — built by [`build-ubuntu-amd64-release.yml`](.github/workflows/build-ubuntu-amd64-release.yml) and arm64 variant
  * Unsigned, uploaded to S3 via `deb-s3`
  * GPG signing in progress: PRs [#5179](https://github.com/ava-labs/avalanchego/pull/5179), [#5180](https://github.com/ava-labs/avalanchego/pull/5180)
* **RPM packages** — built by [`build-rpm-release.yml`](.github/workflows/build-rpm-release.yml)
  * GPG-signed via repo secret (`RPM_GPG_PRIVATE_KEY`)
  * Validated in Rocky Linux 9 container
  * Not uploaded to S3, only GitHub Artifacts
* **Docker images** — built by [`publish_docker_image.yml`](.github/workflows/publish_docker_image.yml)
  * Fully automated, multi-arch, pushed to DockerHub
* **GitHub release page** — created manually via [`create-github-release.sh`](review-notes/create-github-release.sh)
  * Downloads from S3 → signs with GPG → creates release via `gh`
  * Assets: 3 tarballs/zips + 3 detached `.sig` files (6 total)
  * No DEB/RPM packages attached to release page

### Active branches

| Branch | Purpose | PR |
|---|---|---|
| `PlatCore/5109-refactor-rpm-for-reuse-v2` | Parameterize nfpm configs for DEB reuse | [#5179](https://github.com/ava-labs/avalanchego/pull/5179) |
| `PlatCore/5109-add-deb-gpg-signing-v2` | Add GPG-signed DEB packages | [#5180](https://github.com/ava-labs/avalanchego/pull/5180) |
| `PlatCore/5109-add-kms-gpg-signing` | KMS-backed signing for DEB + RPM | [#5136](https://github.com/ava-labs/avalanchego/pull/5136) (draft) |
| `PlatCore/5109-kms-signing-poc` | KMS signing proof of concept | [#5167](https://github.com/ava-labs/avalanchego/pull/5167) (DO NOT MERGE) |

---

## Phase 1: Package Signing Infrastructure

> Issues: [#5160](https://github.com/ava-labs/avalanchego/issues/5160), [#5193](https://github.com/ava-labs/avalanchego/issues/5193)

### 1.1 DEB package signing

* Land nfpm config refactor (PR #5179)
  * Parameterize nfpm configs with env vars
  * Add `DOCKERFILE` parameter for DEB reuse
  * Verify RPM output is unchanged
* Land DEB GPG signing (PR #5180)
  * Dockerfile.deb — Ubuntu 22.04 builder with dpkg-sig
  * build-deb.sh — build + sign flow mirroring RPM pipeline
  * validate-deb.sh — install + signature verification in clean container
  * CI workflow for both jammy and noble, amd64 and arm64

### 1.2 RPM package signing

* Verify existing GPG signing via repo secret works end-to-end
* Ensure RPM validation step covers both architectures

### 1.3 Linux binary tarball signing

* Add GPG detach-sign step to `build-linux-binaries.yml`
  * Sign `.tar.gz` artifacts in CI after build
  * Upload `.sig` files alongside tarballs to S3
  * Verify signatures before upload

---

## Phase 2: macOS Notarization

> Issue: [#5161](https://github.com/ava-labs/avalanchego/issues/5161)

### 2.1 Automate codesign in CI

* Store Apple Developer ID certificate in GitHub secrets
  * Certificate + private key as base64-encoded P12
  * Keychain setup in workflow (create temp keychain, import cert)
* Add codesign step to `build-macos-release.yml`
  * Sign the avalanchego binary
  * Sign the subnet-evm binary
  * Verify signatures with `codesign --verify`

### 2.2 Automate notarization in CI (???)

* Store App Store Connect API key in GitHub secrets
  * Issuer ID, Key ID, AuthKey P8 file
* Add xcrun notarytool step after codesign
  * Create zip for notarization submission
  * Submit via `xcrun notarytool submit --wait`
  * Staple notarization ticket (if applicable for non-.app)
  * Verify with `spctl --assess` or `xcrun notarytool info`

---

## Phase 3: Package Distribution (S3)

> Issues: [#5158](https://github.com/ava-labs/avalanchego/issues/5158), [#5159](https://github.com/ava-labs/avalanchego/issues/5159)

### 3.1 DEB S3 upload

* Update DEB workflow to upload signed `.deb` files to the correct S3 bucket
  * Path: `s3://${BUCKET}/linux/debs/ubuntu/${RELEASE}/${ARCH}/`
  * Both jammy and noble
  * Both amd64 and arm64
* Update `deb-s3` upload to `downloads.avax.network` bucket
  * Publish signed packages to public APT repository

### 3.2 RPM S3 upload

* Add S3 upload step to `build-rpm-release.yml`
  * Path: `s3://${BUCKET}/linux/rpms/${DISTRO}/${ARCH}/`
  * Both x86_64 and aarch64
* [Optional] Publish to a public YUM/DNF repository

### 3.3 Signed tarball + signature upload

* Ensure `build-linux-binaries.yml` uploads both `.tar.gz` and `.tar.gz.sig` to S3
* Ensure `build-macos-release.yml` uploads both `.zip` and `.zip.sig` to S3

---

## Phase 4: GitHub Release Automation

> Issue: [#5162](https://github.com/ava-labs/avalanchego/issues/5162)

### 4.1 Orchestrator workflow

* Create `create-github-release.yml` — top-level workflow triggered by tag push
  * Gate on all build workflows completing successfully
    * `build-linux-binaries.yml`
    * `build-macos-release.yml`
    * `build-ubuntu-amd64-release.yml`
    * `build-ubuntu-arm64-release.yml`
    * `build-rpm-release.yml`
    * `publish_docker_image.yml`
  * Manual workflow_dispatch fallback with tag input

### 4.2 Artifact collection

* Download all signed artifacts from S3 (or from workflow artifacts)
  * Linux tarballs + `.sig` (amd64, arm64)
  * macOS zip + `.sig`
  * DEB packages (jammy, noble × amd64, arm64)
  * RPM packages (x86_64, aarch64)
* Verify checksums / signatures before attaching to release

### 4.3 Release notes generation

* Extract release notes from a conventional source
  * Option A: `CHANGELOG.md` section for the tag
  * Option B: Auto-generate from merged PRs since last tag
  * Option C: Require `release-notes.md` committed alongside version bump PR
* Format release body with artifact table and install instructions

### 4.4 GitHub release creation

* Create release via `gh release create`
  * Title: `{CodeName}.{Patch} - {Description}` (e.g., "Granite.2 - Benchlist Redesign")
  * Attach all artifacts (binaries + signatures + packages)
  * Mark as `latest` (or `prerelease` for `-fuji` tags)
* Expected asset list (target):
  * `avalanchego-linux-amd64-${TAG}.tar.gz` + `.sig`
  * `avalanchego-linux-arm64-${TAG}.tar.gz` + `.sig`
  * `avalanchego-macos-${TAG}.zip` + `.sig`
  * `avalanchego-${TAG}-1.x86_64.rpm`
  * `avalanchego-${TAG}-1.aarch64.rpm`
  * `subnet-evm-${TAG}-1.x86_64.rpm`
  * `subnet-evm-${TAG}-1.aarch64.rpm`
  * `avalanchego_${TAG}_amd64.deb` (jammy, noble)
  * `avalanchego_${TAG}_arm64.deb` (jammy, noble)
  * `subnet-evm_${TAG}_amd64.deb` (jammy, noble)
  * `subnet-evm_${TAG}_arm64.deb` (jammy, noble)

### 4.5 Post-release verification

* Verify asset count and names match expected list
* Spot-check download URLs (HTTP 302)
* Verify `isLatest` flag
* [Optional] Post notification (Slack, etc.)

---

## Phase 5: Observability

> Issue: [#5163](https://github.com/ava-labs/avalanchego/issues/5163)

### 5.1 Replace Datadog links with Grafana

* Identify all Datadog references in release testing docs
* Replace with corresponding Grafana dashboard links
  * `grafana.internal/d/api-latency` (oncall latency dashboard)
  * Other dashboards TBD

### 5.2 Release workflow monitoring

* Add workflow status badges to release documentation
* [Optional] Add Slack notifications for workflow failures
* [Optional] Add Grafana annotations for release events

---

## Phase 6: Validation & Rollout

### 6.1 End-to-end dry run

* Test the full pipeline against a pre-release tag (e.g., `v0.0.0-test`)
  * Verify all artifacts are built, signed, and uploaded
  * Verify GitHub release is created with correct assets
  * Verify DEB/RPM packages install correctly from S3
  * Verify macOS binary passes Gatekeeper checks

### 6.2 Migration from manual process

* Document the new automated process
  * Update the [Rollout Runbook](review-notes/AP-Runbooks-020426-224331-6-.md)
  * Remove manual steps that are now automated
  * Keep manual fallback instructions for emergencies
* Deprecate `create-github-release.sh` after first successful automated release
* Retire internal repo build triggers where superseded by public repo workflows

### 6.3 Security review

* Verify no signing keys are exposed in workflow logs
* Verify OIDC role trust policies are scoped to correct repos/branches
* Verify GPG public keys are distributed for verification

---

## Phase 7: KMS-Backed Signing (Hardening)

> Issue: [#5193](https://github.com/ava-labs/avalanchego/issues/5193)
>
> **Prerequisite:** Phases 1–4 landed and proven with secrets-based signing.

### 7.1 Finalize KMS signing infrastructure

* Finalize KMS signing POC (PR [#5136](https://github.com/ava-labs/avalanchego/pull/5136))
  * AWS OIDC → KMS key access from CI runners
  * Single signing identity for DEB, RPM, and tarball `.sig`
  * Eliminate GPG private key from GitHub secrets

### 7.2 Migrate pipelines to KMS signer

* Update DEB pipeline to use KMS signer
* Update RPM pipeline to use KMS signer
* Update tarball pipeline to use KMS signer
* Verify all artifacts are signed with KMS key
* Update public GPG key distribution

### 7.3 KMS security review

* Verify KMS key policy follows least-privilege
* Verify OIDC trust is scoped to release workflows only
* Verify key rotation plan is documented
* Remove deprecated GPG repo secrets

---

## Dependency Graph

```
Phase 1 (Signing) ──┬──→ Phase 3 (S3 Distribution) ──→ Phase 4 (GH Release)
Phase 2 (macOS)  ───┘                                         │
                                                               ↓
Phase 5 (Observability) ←── independent ──→          Phase 6 (Validation)
                                                               │
                                                               ↓
                                                     Phase 7 (KMS Hardening)
```

## Issue Mapping

| Phase | Issues |
|---|---|
| 1.1 DEB signing | #5193, #5160 |
| 1.2 RPM signing | #5193 |
| 1.3 Tarball signing | #5160 |
| 2.x macOS notarization | #5161 |
| 3.1 DEB S3 upload | #5158 |
| 3.2 RPM S3 upload | #5159 |
| 4.x GitHub release | #5162 |
| 5.x Observability | #5163 |
| 6.x Validation | #5157 (epic) |
| 7.x KMS signing | #5193 |
