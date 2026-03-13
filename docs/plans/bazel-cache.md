# Bazel Caching Plan

## Context

The repository currently uses Bazel's local disk cache via
`.bazelrc`:

```text
build --disk_cache=~/.cache/bazel-disk-cache
```

That improves repeat builds on a single machine, but it does not let
CI or other Linux developers reuse work already performed elsewhere.
The immediate need is to speed up Linux-based Bazel CI and allow a
small set of Linux developers to read from the same cache. Most
developers use macOS, so near-term shared caching only needs to work
for Linux.

The design should preserve a conservative trust model:

- Linux CI can read and write the shared cache
- Linux developers can read from the shared cache
- macOS developers continue using only local cache for now
- developer write access is intentionally deferred to reduce risk from
  non-hermetic local environments

## Goals

- Speed up Bazel CI by reusing build/test outputs across workflow runs
- Allow selected Linux developers to read from the same cache used by
  CI
- Keep cache policy simple: CI writes, developers read
- Reuse the same cache setup path for standard Bazel jobs and the e2e
  job, which performs extra setup before invoking Bazel
- Validate the cache service independently before changing CI jobs

## Non-Goals

- Shared caching for macOS developers
- Remote execution
- Fine-grained per-team or per-target cache policy
- Supporting arbitrary developer laptops as cache writers

## Chosen Approach

Deploy a Bazel remote cache service in EKS, backed by S3, and have
Bazel talk to it natively via `--remote_cache=...`.

Expected policy:

- CI on Linux: read/write
- selected Linux developers: read-only
- macOS developers: local disk cache only

This is preferable to syncing the local Bazel disk cache to S3 because
it uses Bazel's native remote cache protocol rather than layering a
custom restore/save scheme around a local cache directory.

### Why This Approach Was Selected

1. Bazel is designed to use a remote cache service directly.
   Native remote caching is a first-class Bazel workflow; syncing
   `~/.cache/bazel-disk-cache` to object storage is not.
2. It supports the desired trust model cleanly.
   Bazel can read from a remote cache without uploading local results,
   which maps directly to "developers read, CI writes."
3. It works for both standard CI jobs and e2e.
   The cache configuration can be injected once before any workflow
   path that eventually invokes Bazel.
4. It keeps local and CI behavior conceptually aligned.
   Both use the same Bazel remote cache flags, even if only CI writes.
5. It avoids unnecessary cache churn.
   Syncing whole local cache directories would transfer unrelated local
   state and is a poor fit for ephemeral CI runners.

## Alternatives Considered

### 1. Continue Using Only Local `--disk_cache`

Not selected because it provides no sharing between CI runs or Linux
developers.

### 2. Sync the Local Bazel Disk Cache to S3

Example shape:

- restore `~/.cache/bazel-disk-cache` from S3 before Bazel runs
- run Bazel locally against `--disk_cache`
- sync the directory back to S3 after successful CI runs

Not selected because:

- it is not Bazel's native shared-cache model
- object transfer would be coarse and inefficient
- it creates custom cache lifecycle logic in repo scripts/workflows
- it is harder to reason about correctness than Bazel-native remote
  cache uploads/downloads
- it would still require extra policy logic for CI-writes-only

This remains a fallback option only if an HTTP/gRPC cache service
cannot be deployed soon.

### 3. Let Developers Write to the Shared Cache

Not selected for the initial rollout.

The repo includes CGO and non-trivial shell-script-driven workflows,
which makes it unwise to assume perfect hermeticity across developer
machines. CI runs in a narrower, more controlled Linux environment, so
it is the safer initial writer.

Developer write access can be revisited after:

- the cache is operating reliably
- hit rates are understood
- the team has confidence in hermeticity for the relevant Bazel paths

### 4. Build a Shared Cache for macOS at the Same Time

Not selected because the immediate payoff is low. The main consumers of
the shared cache are Linux CI and a small number of Linux developers.
macOS can continue using local cache until there is enough demand to
justify a separate Darwin cache namespace and provisioning path.

### 5. Use Remote Execution Instead of Remote Caching

Not selected because the current objective is cache reuse, not moving
build execution off-host. Remote execution would be a much larger
infrastructure and rule-hermeticity project.

## High-Level Design

### Cache Topology

- Bazel client in CI or on a Linux developer host
- remote cache service reachable over HTTP/gRPC
- backing object storage in S3
- optional persistent disk or in-memory layer on the cache service,
  depending on the chosen server implementation

### Access Model

- CI authenticates with a role that allows cache writes
- Linux developers authenticate with a role that allows cache reads
- macOS developers do not use the shared remote cache

### Repository Integration

Introduce a dedicated setup path for Bazel cache configuration:

- add a shared GitHub Action, likely
  `.github/actions/setup-bazel-cache`
- have `.github/actions/run-bazel-task/action.yml` call it before
  running the task
- have `.github/workflows/bazel-ci.yml` call it in the e2e job before
  invoking `.github/actions/run-monitored-tmpnet-cmd/action.yml`
- teach `scripts/run_bazel.sh` to honor the same cache-related env vars
  for local Linux developer usage

The setup action should configure Bazel-native remote cache flags and
auth-related environment, not perform ad hoc disk-cache restore/save
operations.

## Recommended Server Shape

Use a small remote cache service such as `bazel-remote` running in EKS,
with S3 as durable backing storage.

Desired characteristics:

- simple HTTP remote cache support for Bazel
- straightforward deployment and observability
- compatible with S3-backed persistence
- supports small initial scale and easy iteration

This plan does not lock the implementation to a specific container
image today, but `bazel-remote` is the expected default unless a
different internally preferred cache service already exists.

## EKS Deployment Plan

Validate the cache server in EKS before changing CI jobs. That reduces
iteration time because infrastructure problems can be solved
independently from GitHub Actions and Bazel workflow wiring.

### Infrastructure Requirements

- an EKS cluster reachable from GitHub Actions runners and developer
  Linux hosts, or exposed through an acceptable ingress path
- an S3 bucket/prefix dedicated to Bazel cache objects
- IAM roles for:
  - cache server access to S3
  - CI write access if the cache server requires client auth beyond
    network reachability
  - developer read access if client auth is required
- DNS name for the cache endpoint
- TLS termination strategy
- basic metrics and logs

### Kubernetes Components

- `Deployment` for the cache server
- `Service` exposing the cache server inside the cluster
- `Ingress` or `LoadBalancer` service for external access
- `ServiceAccount` annotated for IRSA access to S3
- `ConfigMap` or container args defining cache size, S3 backing config,
  listen address, and GC behavior
- `PodDisruptionBudget` if availability during node events matters
- optional `HorizontalPodAutoscaler` if concurrency demands it

### EKS Validation Before CI Integration

1. Deploy the cache service to a non-production namespace.
2. Verify the service can read/write its S3 backing store.
3. Run a Bazel client manually from a Linux host against the endpoint.
4. Confirm cache hits on a second identical Bazel invocation.
5. Confirm read-only clients can download but not upload.
6. Confirm cache behavior remains correct across pod restarts.
7. Confirm observability is sufficient to diagnose misses, errors, and
   storage growth.

### Suggested Manual Validation Workflow

From a Linux machine with Bazel:

1. Run a representative Bazel build with remote upload enabled against
   the EKS endpoint.
2. Clear local output state while preserving no useful local cache.
3. Re-run the same build and verify remote hits are observed.
4. Repeat with a read-only credential set and confirm:
   - build succeeds
   - remote hits are visible
   - uploads are rejected or suppressed

### Operational Considerations

- define S3 lifecycle rules to manage long-term object growth
- define server-side eviction/GC policy if the chosen cache server uses
  a local working set
- document endpoint ownership and on-call expectations
- decide whether cache invalidation requires manual support procedures
- ensure the service is isolated from public abuse if exposed beyond
  the corporate network

## Repository Rollout Plan

### Phase 1: Infrastructure Validation

- provision the cache bucket/prefix
- deploy the cache service to EKS
- validate end-to-end read/write from Linux against the service
- validate read-only behavior for non-CI clients
- capture the final cache endpoint, auth model, and operational limits

Exit criteria:

- repeated Linux Bazel builds demonstrate cache hits
- S3 backing storage is confirmed functional
- CI and developer access model is decided and tested

### Phase 2: Repository Support for Cache Configuration

- add `.github/actions/setup-bazel-cache`
- make it configure Bazel remote cache env/flags and any required auth
- update `.github/actions/run-bazel-task/action.yml` to invoke the new
  setup action
- update `.github/workflows/bazel-ci.yml` so the e2e job also uses the
  setup action before entering the monitored tmpnet path
- update `scripts/run_bazel.sh` to honor the same env vars for local
  Linux use
- document Linux developer setup in `docs/bazel.md`

Exit criteria:

- standard Bazel CI jobs use the remote cache
- e2e reaches Bazel with the same cache configuration
- Linux developers can opt in to read-only shared cache usage

### Phase 3: CI Policy and Measurement

- configure CI with write-capable credentials
- configure developer docs/examples with read-only settings
- measure cache hit rates and job duration changes
- check for correctness regressions or suspicious cache behavior

Exit criteria:

- CI duration improvement is measurable
- cache error rate is low
- no evidence of incorrect cache reuse

## Validation Criteria

Success means all of the following are true:

- a Bazel remote cache service is deployed and reachable
- Linux CI jobs use it successfully
- cache hits are visible on repeated CI runs
- e2e uses the same cache setup path before invoking Bazel
- Linux developers can opt in to read-only shared cache usage
- macOS developers remain unaffected and continue using local cache
- documentation explains the chosen trust model and setup steps

## Open Questions

- Which exact cache service should be used in EKS: `bazel-remote` or an
  existing internal alternative?
- What client authentication model is preferred for the cache
  endpoint: network-level trust only, basic auth, mTLS, or another
  mechanism?
- Will GitHub Actions reach the endpoint directly, or via VPN/private
  networking?
- Does the cache endpoint need separate namespaces or prefixes for
  different Bazel versions or major repo changes?
- What observability stack should own cache metrics and alerts?

## Follow-Up Work

- add a Darwin-specific shared cache path if macOS demand increases
- reconsider developer write access after the Linux cache has proven
  stable
- evaluate whether remote execution is worthwhile after remote caching
  has matured
