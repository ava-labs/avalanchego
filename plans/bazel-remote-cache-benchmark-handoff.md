# Bazel remote cache benchmark handoff

## Goal
Maintain and extend a Bazel remote-cache benchmark harness that models CI-style behavior closely enough to answer:
- how much remote action caching helps on real CI runners
- whether that payoff changes when build work increases (for example, Firewood-from-source)

## Current state
There are now two benchmark entrypoints:

- Generalized CI-style benchmark:
  - task: `bazel-benchmark-remote-cache`
  - script: `scripts/benchmark_bazel_remote_cache.sh`
- Fast smoke benchmark:
  - task: `bazel-benchmark-remote-cache-ids-test`
  - script: `scripts/benchmark_bazel_remote_cache_ids_test.sh`

There is also supporting documentation and CI wiring:
- docs: `docs/bazel.md`
- workflow: `.github/workflows/bazel-ci.yml`

## What the generalized benchmark does now
The generalized benchmark is task-configured with:
- setup: `fetch --all`
- benchmarks:
  - `test //... -- -//graft/...`

For each benchmark command it currently measures:
1. no remote cache
2. cold remote cache
3. warm remote cache

Important behavior:
- measured setup phase first
- fresh `--output_base` for every measured run
- `--disk_cache=` to disable local disk cache
- per-benchmark isolated temporary `bazel-remote` dir so each cold/warm pair is actually cold for that command
- strict failure unless warm cache is faster than both no-cache and cold-cache
- keeps temp workspace on failure for inspection
- deletes successful output bases eagerly to reduce peak disk usage
- measures representative cache latency at benchmark startup unless overridden

## Important implementation detail discovered after initial rollout
`repository_cache` alone was not sufficient for fresh-output-base reuse of Gazelle `go_repository` dependencies.

This showed up clearly on macOS CI in PR `5530`:
- setup `bazel fetch //...` succeeded
- fresh no-cache build succeeded
- fresh cold-remote build failed trying to resolve `proxy.golang.org`
- the failing dependency in that run was `github.com/kr/pretty`

The cause is that Gazelle `go_repository` normally keeps a per-output-base internal Go module cache, so a fresh `--output_base` can still trigger fresh Go module downloads even after `bazel fetch //...`.

### Fix now in place
Both benchmark scripts now force Gazelle to share a host-level Go module cache across setup and measured runs by passing:
- `--repo_env=GO_REPOSITORY_USE_HOST_MODCACHE=1`
- `--repo_env=GOMODCACHE=<shared temp dir>`

This is in addition to the shared Bazel `repository_cache`.

Conceptually:
- `repository_cache` handles Bazel-managed downloads
- shared `GOMODCACHE` handles Gazelle `go_repository` module downloads
- fresh `output_base` still isolates local action/output state

This was the key missing piece needed to make the setup phase actually reduce repeated Go-module downloading across fresh output bases.

## Current CI wiring
`.github/workflows/bazel-ci.yml` now includes a non-required observational job:
- job: `benchmark-remote-cache`
- runs on:
  - `linux-amd64`
  - `darwin-arm64`
- uses `./.github/actions/install-nix`
- runs the benchmark step with:
  - `shell: nix develop --command bash -x {0}`
- uploads the full benchmark log as an artifact
- writes a step-summary snippet

This job is intentionally not part of the required Bazel aggregate.

## Known CI history relevant to debugging
### Earlier workflow mistake
An earlier revision used `install-nix` but still ran `./scripts/run_task.sh ...` outside `nix develop`, which failed because `go`/repo tools were not on `PATH`.

The workflow now follows the repo pattern from `.github/workflows/ci.yml` by using a nix shell for the step itself.

### macOS benchmark failure in PR 5530
The important failure was not the nix setup; it was the benchmark itself still attempting Go-module network resolution from a fresh output base.

That failure is what motivated the shared `GOMODCACHE` change.

If a future session sees a similar macOS failure, first verify whether the benchmark run includes both:
- `--repo_env=GO_REPOSITORY_USE_HOST_MODCACHE=1`
- `--repo_env=GOMODCACHE=...`

## Validated local results
### Generalized benchmark
The generalized harness still works as a three-way benchmark and now also
measures representative latency before the timed runs begin.

Validated local behaviors:
- setup uses `fetch --all`
- benchmark command is currently `test //... -- -//graft/...`
- default mode measures representative latency with
  `bazel run //tools/measure-http-latency`
- override mode works with `BAZEL_REMOTE_CACHE_LATENCY_MS=<positive-ms>`
- benchmark timing still behaves as expected for no-cache / cold / warm runs

These numbers are host-specific; the important point is that the generalized
flow now completes successfully with fresh output bases while reusing both
repository cache and Gazelle module cache, and that it has a live latency
measurement input ready for the next toxiproxy step.

### Smoke benchmark
Local run of `task bazel-benchmark-remote-cache-ids-test` still passes and now
also proves cached test-result reuse via Bazel output indicating the warm run
executed `0 out of 1` tests.

## Why the benchmark is structured this way
The benchmark is intentionally trying to model:
- one setup phase per runner/architecture
- many later jobs that start with fresh local Bazel state
- reused downloaded external dependencies
- reused remote action results only when a remote cache is configured

The setup phase therefore exists to answer:
- “what can we download/materialize once per architecture?”

The measured benchmark phases then answer:
- “given that setup, how much does remote action caching help?”

## Why this matters for Firewood work
The next intended comparison is not just cache/no-cache in isolation, but a matrix like:
- baseline branch vs Firewood-from-source branch
- no remote cache vs cold remote cache vs warm remote cache
- normal build vs race build
- same CI runner class

The expectation is that if Firewood-from-source adds meaningful build work, the relative payoff of warm remote cache may increase.

## What a future session should read first
Before changing anything, read:
- `plans/bazel-remote-cache-benchmark-handoff.md`
- `scripts/benchmark_bazel_remote_cache.sh`
- `scripts/benchmark_bazel_remote_cache_ids_test.sh`
- `tools/measure-http-latency/main.go`
- `docs/bazel.md`
- `.github/workflows/bazel-ci.yml`
- PR `5525`
- PR `5530`

Important: this handoff document is meant to evolve with the work. Treat it as
session-to-session source of truth, but verify key implementation details in the
current files before assuming older sections are still accurate.

## Current files of interest
- `Taskfile.yml`
- `.bazelignore`
- `docs/bazel.md`
- `.github/workflows/bazel-ci.yml`
- `scripts/benchmark_bazel_remote_cache.sh`
- `scripts/benchmark_bazel_remote_cache_ids_test.sh`
- `tools/measure-http-latency/main.go`

## Commits created during this work
Relevant local commits on this branch include:
- `3770c122ed` — `Add Bazel remote cache benchmark task`
- `e653082029` — `Add generalized Bazel remote cache benchmark`
- `02d0e61dd4` — `Document and publish Bazel cache benchmark`
- `9487b5d4a1` — `Reduce benchmark output-base disk usage`
- `b1c2624d7a` — `Share Gazelle module cache across benchmark runs`

## PR result summary for future comparison
### PR 5525 — baseline branch cache-writer rollout
- PR: `5525` — https://github.com/ava-labs/avalanchego/pull/5525
- Bazel workflow run: `1381` / Actions run `27390142678`
- Important limitation: this PR predates the observational `benchmark-remote-cache` job, so there are **no no-cache / cold-remote / warm-remote benchmark artifacts** to extract from CI for this baseline branch.
- What CI did record is the earlier cache-writer shape: each platform's `check-metadata` job ran a `Prefetch Bazel external dependencies` step after metadata verification.

Closest available CI timing data from PR 5525:

| PR | branch role | platform | prefetch external deps | unit-main | unit-coreth | unit-subnet-evm | e2e |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 5525 | baseline | linux-amd64 | 26s | 10m15s | 9m17s | 6m53s | 7m47s |
| 5525 | baseline | darwin-arm64 | 21s | 11m19s | 8m58s | 8m58s | 10m18s |
| 5525 | baseline | linux-arm64 | 22s | 9m15s | 6m19s | 5m26s | 6m36s |

Interpretation:
- PR 5525 validated the **dependency seeding** part of the CI model.
- It does **not** provide remote-action-cache benchmark numbers comparable to PR 5530.

### PR 5530 — Firewood-from-source benchmark rollout
- PR: `5530` — https://github.com/ava-labs/avalanchego/pull/5530
- Bazel workflow run: `1443` / Actions run `27456887507`
- This PR is the first one that published the generalized CI-style remote-cache benchmark as CI artifacts.
- Both observational benchmark jobs passed:
  - `linux-amd64 / benchmark-remote-cache`
  - `darwin-arm64 / benchmark-remote-cache`
- The macOS run completed successfully after the shared `GOMODCACHE` change, which is the key evidence that the earlier fresh-output-base Go-module fetch failure was fixed.

Extracted benchmark results from PR 5530 artifacts:

| PR | branch role | platform | setup `fetch //...` | command | no cache | cold remote | warm remote |
| --- | --- | --- | ---: | --- | ---: | ---: | ---: |
| 5530 | firewood-from-source | linux-amd64 | 74.419s | `build //main:avalanchego` | 370.858s | 381.428s | 54.919s |
| 5530 | firewood-from-source | linux-amd64 | 74.419s | `build --config=race //main:avalanchego` | 368.786s | 380.012s | 54.225s |
| 5530 | firewood-from-source | darwin-arm64 | 86.115s | `build //main:avalanchego` | 303.376s | 309.079s | 55.305s |
| 5530 | firewood-from-source | darwin-arm64 | 86.115s | `build --config=race //main:avalanchego` | 291.600s | 302.801s | 56.121s |

Observed savings from PR 5530 warm-cache runs:
- linux-amd64:
  - normal build: `370.858s -> 54.919s` (~85% reduction)
  - race build: `368.786s -> 54.225s` (~85% reduction)
- darwin-arm64:
  - normal build: `303.376s -> 55.305s` (~82% reduction)
  - race build: `291.600s -> 56.121s` (~81% reduction)

### Comparative takeaway
- PR 5525 gives the **baseline dependency-cache CI shape**, but not full remote-cache benchmark data.
- PR 5530 gives the first full **CI benchmark artifact set** for the Firewood-from-source branch.
- Therefore the current cross-PR comparison is necessarily asymmetric:
  - **5525**: only setup/prefetch-style CI timings are available
  - **5530**: full setup + no-cache/cold-cache/warm-cache benchmark timings are available
- If a like-for-like baseline-vs-firewood benchmark table is still needed, the remaining work is to run the PR 5530 benchmark harness on a baseline branch that has the post-`GOMODCACHE` benchmark implementation.

## Next investigation: representative-latency HTTP vs gRPC cache comparison
The next step is to evolve the generalized benchmark from a simple
“remote cache helps or not” experiment into a protocol and latency
comparison that better reflects the intended deployment shape.

### New question
The benchmark should answer:
- how much remote-cache latency changes the payoff of cache hits
- whether HTTP remote caching is sufficient under representative CI-to-cache latency
- whether gRPC provides enough additional value to justify requiring it

This is explicitly a cache-behavior experiment, not a transport
microbenchmark. The point is to understand the effect of realistic cache
distance on Bazel execution time and to compare the practical value of
HTTP vs gRPC remote caching.

### Latency model
The benchmark should no longer assume an effectively zero-latency local
cache server.

Current implementation status:
1. representative latency is already measured at benchmark startup by default
2. the latency source is a public AWS us-east-1 regional endpoint
   (`https://ec2.us-east-1.amazonaws.com/` by default)
3. the measured value used as the coarse latency input is average TTFB
4. an explicit positive `BAZEL_REMOTE_CACHE_LATENCY_MS` override skips live
   measurement for fast local iteration
5. a tolerance knob exists for future validation:
   `BAZEL_REMOTE_CACHE_LATENCY_TOLERANCE_MS`

Current design choices:
- use a public AWS us-east-1 regional endpoint as the first-pass latency
  approximation because it is available before a real deployed bazel-remote
  exists
- use average TTFB from the latency probe as the coarse latency input because
  it best approximates the cost of small cache requests
- measure latency live by default to avoid stale hard-coded values as runner
  routing and network conditions change
- allow an explicit latency override for fast local iteration, which skips the
  provenance-oriented live measurement and any future proxy-path validation

Trade-off:
- this is intended to approximate runner-to-us-east-1 regional distance, not
  to claim exact equivalence with a future EKS-hosted bazel-remote deployment
- measuring against a real deployed bazel-remote is the next planned
  refinement once the first-pass protocol comparison exists

### Intended benchmark matrix
Longer-term, for each benchmark command, the intended comparison becomes:
- no cache
- HTTP remote cache, cold
- HTTP remote cache, warm
- gRPC remote cache, cold
- gRPC remote cache, warm

All cached runs should use the same induced representative latency so the
only major variable between the cached cases is the cache protocol.

This work is intentionally split into phases:
1. establish representative latency measurement during benchmark setup
2. feed that measured latency into `toxiproxy` and verify that the local cache
   path actually experiences the modeled delay
3. once latency injection is validated, route benchmarked HTTP cache traffic
   through the proxy
4. only after the HTTP path is proven, add the gRPC comparison

Step 1 is complete.

## Immediate next implementation step
The next session should wire `toxiproxy` into the setup phase of
`scripts/benchmark_bazel_remote_cache.sh`.

Concretely, the next step should:
1. start `bazel-remote` with an HTTP listener enabled
2. start `toxiproxy`
3. create a proxied HTTP endpoint in front of `bazel-remote`
4. apply induced latency using the already-measured representative latency
5. in default measured mode, validate that the proxied HTTP path is within
   `BAZEL_REMOTE_CACHE_LATENCY_TOLERANCE_MS` of the measured target
6. in override mode, skip live measurement and skip proxy-path validation
7. use only the `//ids:ids_test` smoke benchmark while iterating on this work
8. stop there; do not yet change the generalized benchmark run matrix

The purpose of that next step is only to prove that representative latency can
be injected and observed reliably. It does not yet need to benchmark cache
behavior through the proxy. Keeping the validation loop scoped to
`//ids:ids_test` minimizes runtime and speeds up iteration while the proxying
mechanics are still being established.

## Follow-up step after toxiproxy validation
Once latency injection is validated in setup:
1. keep reusing the existing Bazel repository cache and shared Gazelle module
   cache during iteration so setup work does not dominate feedback time
2. route the existing HTTP cold/warm benchmark traffic through the proxied
   endpoint
3. confirm the benchmark still behaves sensibly under induced latency
4. only then enable the gRPC listener and expand the comparison matrix to:
   - no cache
   - HTTP cold
   - HTTP warm
   - gRPC cold
   - gRPC warm

### Decision this should inform
This comparison is meant to guide the practical choice between cache
protocols:
- if HTTP warm-cache performance remains close to gRPC under realistic
  latency, HTTP may be operationally sufficient
- if HTTP degrades materially while gRPC remains strong, that is evidence
  that gRPC may be worth requiring
- if realistic cache latency erodes most of the value of both protocols,
  that changes the overall case for remote caching in CI

### Validation criteria
A successful next iteration should:
- measure and record representative CI-to-us-east-1 latency separately
- feed that measured latency into `toxiproxy`
- inject that latency into benchmarked cache traffic
- produce timings for no-cache / HTTP cold-warm / gRPC cold-warm
- make it easy to compare protocol behavior under the same latency model

## Open questions worth keeping in mind
- Is `fetch --all` the best possible setup command, or is there a narrower/faster command that still fully seeds the dependency state we care about?
- Should the observational benchmark remain non-required forever, or become a required informational job later?
- If CI runner networking remains flaky even after shared `GOMODCACHE`, should the benchmark artifact upload step become more failure-tolerant, or should the benchmark be temporarily scoped by platform?
