# Bazel Remote Cache Benchmark Findings

This document summarizes the current conclusion from the CI-style Bazel remote
cache benchmark harness.

## Question answered
On GitHub Actions, for AvalancheGo's current Bazel workflow shape:
- how much does warm remote caching help?
- does impacted-target execution add meaningful value on top of warm cache?

## Scope
The benchmark models:
- one setup phase per runner/architecture
- fresh Bazel `--output_base` per measured run
- shared setup-side dependency caches
- remote-cache comparisons under representative injected latency

Measured CI slices:
- build cache matrix
- test cache matrix
- empty impacted-target comparison
- non-empty impacted-target comparison

Platforms measured:
- `linux-amd64`
- `darwin-arm64`

Latest successful reference run:
- GitHub Actions run `27788833041`

## Bottom line
Warm remote cache is the dominant win.

Impacted-target execution adds little incremental value on top of warm cache in
this static-job GitHub Actions workflow:
- empty impacted set:
  - modest win on Linux
  - roughly break-even on macOS
- non-empty impacted set:
  - slower than a warm full-scope cached rerun on both platforms

So for the current GitHub Actions shape, the main value is the remote cache
itself, not impacted-target execution inside already-created jobs.

## Key results

### Build matrix
#### linux-amd64
- setup: `48.829s`
- `build //main:avalanchego`
  - no cache: `174.391s`
  - HTTP warm: `52.415s`
  - gRPC warm: `55.441s`
- `build --config=race //main:avalanchego`
  - no cache: `169.355s`
  - HTTP warm: `51.438s`
  - gRPC warm: `54.472s`

#### darwin-arm64
- setup: `50.180s`
- `build //main:avalanchego`
  - no cache: `143.606s`
  - HTTP warm: `46.237s`
  - gRPC warm: `45.325s`
- `build --config=race //main:avalanchego`
  - no cache: `143.064s`
  - HTTP warm: `45.520s`
  - gRPC warm: `45.982s`

### Test matrix
#### linux-amd64
- setup: `61.040s`
- no cache: `696.405s`
- HTTP warm: `155.860s`
- gRPC warm: `174.868s`

#### darwin-arm64
- setup: `73.915s`
- no cache: `817.402s`
- HTTP warm: `145.244s`
- gRPC warm: `176.204s`

### Empty impacted-target comparison
#### linux-amd64
- HTTP warm full rerun: `84.632s`
- impacted total: `69.433s`
- selected targets: `0`

#### darwin-arm64
- HTTP warm full rerun: `81.958s`
- impacted total: `82.114s`
- selected targets: `0`

### Non-empty impacted-target comparison
An earlier catastrophic result here was caused by a harness bug: impacted
execution was talking to a dead remote-cache endpoint because `bazel-remote`
was stopped too early. That bug is fixed.

#### linux-amd64
- HTTP warm full rerun: `74.683s`
- impacted total: `119.944s`
- selected targets: `187`

#### darwin-arm64
- HTTP warm full rerun: `150.824s`
- impacted total: `211.801s`
- selected targets: `187`

## Interpretation
- Warm remote caching is a large win for both builds and tests.
- HTTP and gRPC are both effective; in these test runs, warm HTTP was at least
  as good as warm gRPC.
- Once the full-scope test rerun is mostly remote-cache hits, selector cost plus
  separate impacted execution usually does not pay for itself.

## Firewood-from-source context
PR `5530` (`Add Bazel remote cache benchmark task for firewood`) validated the
same core point on a Firewood-from-source branch, where Bazel builds were much
more expensive.

Reference run:
- GitHub Actions run `27456887507`

Build-only benchmark results from that run:

### linux-amd64
- setup: `74.419s`
- `build //main:avalanchego`
  - no cache: `370.858s`
  - HTTP warm: `54.919s`
- `build --config=race //main:avalanchego`
  - no cache: `368.786s`
  - HTTP warm: `54.225s`

### darwin-arm64
- setup: `86.115s`
- `build //main:avalanchego`
  - no cache: `303.376s`
  - HTTP warm: `55.305s`
- `build --config=race //main:avalanchego`
  - no cache: `291.600s`
  - HTTP warm: `56.121s`

Compared with the current non-Firewood branch measurements, the uncached build
cost on PR `5530` was roughly 2x higher, while warm-cache reruns remained in
roughly the same ~45-56s range.

That means remote caching is especially valuable when build work increases: it
turns a branch that is dramatically more expensive without cache into one whose
warm reruns are still close to the normal branch.

## Important caveat
This conclusion is specific to static-job GitHub Actions.

A dynamic CI system that can use impact analysis to avoid creating jobs at all
could still get more value from impacted-target selection.

## Related files
- `docs/bazel.md`
- `plans/bazel-remote-cache-benchmark-handoff.md`
- `scripts/benchmark_bazel_remote_cache.sh`
- `.github/workflows/bazel-ci.yml`
