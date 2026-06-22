# Bazel remote cache benchmark handoff

## Goal
Maintain and extend a Bazel remote-cache benchmark harness that models CI-style behavior closely enough to answer:
- how much remote action caching helps on real CI runners
- whether that payoff changes when build work increases (for example, Firewood-from-source)

## Current state
There are now two benchmark entrypoints, both backed by the same generalized
script:

- Generalized CI-style benchmark:
  - task: `bazel-benchmark-remote-cache`
  - script: `scripts/benchmark_bazel_remote_cache.sh`
- Fast smoke benchmark:
  - task: `bazel-benchmark-remote-cache-ids-test`
  - script: `scripts/benchmark_bazel_remote_cache.sh`

There is also supporting documentation and CI wiring:
- docs: `docs/bazel.md`
- benchmark findings: `docs/bazel-remote-cache-benchmark.md`
- workflow: `.github/workflows/bazel-ci.yml`

The CI benchmark job is currently split into four matrix slices per supported platform:
- `builds`
- `tests`
- `impacted-empty`
- `impacted-nonempty`

## What the generalized benchmark does now
The generalized benchmark is task-configured with:
- setup: `fetch //main:avalanchego //... -- -//graft/...`
- benchmarks:
  - `build //main:avalanchego`
  - `build --config=race //main:avalanchego`
  - `test //... -- -//graft/...`

For each benchmark command it currently measures:
1. no remote cache
2. cold HTTP remote cache
3. warm HTTP remote cache
4. cold gRPC remote cache
5. warm gRPC remote cache

Important behavior:
- measured setup phase first
- setup intentionally avoids `fetch --all`; it hard-codes the current union of benchmark target patterns because `fetch --all` pulled irrelevant transitive repos and toolchains unrelated to the benchmarked jobs
- fresh `--output_base` for every measured run
- `--disk_cache=` to disable local disk cache
- per-benchmark isolated temporary `bazel-remote` dir so each protocol-specific cold/warm pair is actually cold for that command
- strict failure unless each warm cached run is faster than both no-cache and its corresponding cold cached run
- keeps temp workspace on failure for inspection
- deletes successful output bases eagerly to reduce peak disk usage
- measures representative cache latency at benchmark startup unless overridden
- starts a temporary `bazel-remote` + `toxiproxy` validation path before the measured Bazel runs begin
- in default measured mode, validates that the proxied HTTP path stays within `BAZEL_REMOTE_CACHE_LATENCY_TOLERANCE_MS` of the measured target
- benchmarked HTTP and gRPC cache traffic both flow through toxiproxy latency injection under the same representative latency model
- in override mode, still creates the proxied path but skips live measurement and proxy-path validation

## Important implementation detail discovered after initial rollout
`repository_cache` alone was not sufficient for fresh-output-base reuse of Gazelle `go_repository` dependencies.

This showed up clearly on macOS CI in PR `5530`:
- setup `bazel fetch //...` succeeded
- fresh no-cache build succeeded
- fresh cold-remote build failed trying to resolve `proxy.golang.org`
- the failing dependency in that run was `github.com/kr/pretty`

The cause is that Gazelle `go_repository` normally keeps a per-output-base internal Go module cache, so a fresh `--output_base` can still trigger fresh Go module downloads even after `bazel fetch //...`.

### Current fix in place
The benchmark harness forces Gazelle to share a host-level Go module cache
across setup and measured runs with:
- `--repo_env=GO_REPOSITORY_USE_HOST_MODCACHE=1`
- `--repo_env=GOMODCACHE=<shared cache dir>`

Conceptually:
- `repository_cache` handles Bazel-managed downloads
- shared `GOMODCACHE` handles Gazelle `go_repository` module downloads
- fresh `output_base` still isolates local action/output state

This is current behavior in the shared benchmark script and should be treated
as a required part of the harness rather than merely historical context.

## Current CI wiring
`.github/workflows/bazel-ci.yml` now includes a non-required observational job:
- job: `benchmark-remote-cache`
- runs on:
  - `linux-amd64`
  - `darwin-arm64`
- is split into four matrix slices:
  - `builds`
  - `tests`
  - `impacted-empty`
  - `impacted-nonempty`
- uses `./.github/actions/install-nix`
- prunes runner disk only on `linux-amd64`
- runs the benchmark step with:
  - `shell: nix develop --command bash -x {0}`
- uploads a directory artifact containing:
  - `summary.log`
  - `diagnostics/logs/*`
  - `diagnostics/metadata.txt`
- writes a step-summary snippet

This job is intentionally not part of the required Bazel aggregate.

## Known CI history relevant to debugging
### Earlier workflow mistake
An earlier revision used `install-nix` but still ran `./scripts/run_task.sh ...` outside `nix develop`, which failed because `go`/repo tools were not on `PATH`.

The workflow now follows the repo pattern from `.github/workflows/ci.yml` by using a nix shell for the step itself.

### macOS benchmark failure in PR 5530
The important failure was not the nix setup; it was the benchmark itself still attempting Go-module network resolution from a fresh output base.

That failure is what motivated the shared `GOMODCACHE` support that is now back
in the scripts.

If a future session sees a similar macOS failure, first verify whether the current benchmark run still includes both:
- `--repo_env=GO_REPOSITORY_USE_HOST_MODCACHE=1`
- `--repo_env=GOMODCACHE=...`

## Validated local results
### Generalized benchmark
The generalized harness now measures representative latency before the timed
runs begin, validates a proxied HTTP path to `bazel-remote` against that
measured target, and then benchmarks both HTTP and gRPC remote-cache traffic
through toxiproxy under the same latency model.

Validated local behaviors:
- task-configured generalized benchmark setup is now `fetch //main:avalanchego //... -- -//graft/...`
- task-configured generalized benchmark commands are currently:
  - `build //main:avalanchego`
  - `build --config=race //main:avalanchego`
  - `test //... -- -//graft/...`
- default mode measures representative latency with
  `bazel run //tools/measure-http-latency`
- default mode also starts `bazel-remote` + `toxiproxy`, applies the measured
  latency to a proxied HTTP endpoint, and verifies the observed proxied TTFB is
  within `BAZEL_REMOTE_CACHE_LATENCY_TOLERANCE_MS` of the target
- current validation probes the proxied `bazel-remote` HTTP root only to
  measure request latency; the exact HTTP status code is not the signal of
  interest there, only the observed TTFB through the proxy path
- override mode works with `BAZEL_REMOTE_CACHE_LATENCY_MS=<positive-ms>` and
  skips both live latency measurement and proxy-path validation
- the script now measures no-cache / HTTP cold-warm / gRPC cold-warm for each
  benchmark command
- benchmark timing still behaves as expected for those five cases on the smoke
  target

These numbers are host-specific; the important point is that the validated
local smoke flow now has live latency measurement, a validated proxied HTTP
path, and benchmarked HTTP and gRPC cache traffic flowing through toxiproxy.

### Smoke benchmark
Local run of `task bazel-benchmark-remote-cache-ids-test` still passes and now
also proves cached test-result reuse via Bazel output indicating the warm runs
executed `0 out of 1` tests. That smoke validation now covers both the warm
HTTP and warm gRPC benchmark cases because both reuse the same shared script and
warm-log assertions.

The fast `ids_test` smoke task should stay as a thin parameterization of the
same generalized harness so shared-setup-cache behavior stays aligned by
construction rather than by manual duplication.

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
- no remote cache vs HTTP cold/warm vs gRPC cold/warm
- normal build vs race build
- same CI runner class

The expectation is that if Firewood-from-source adds meaningful build work, the relative payoff of warm remote cache may increase.

Current directionally useful takeaway: even before introducing affected-target
reduction, the benchmarked full-workload jobs already appear capable of at
least roughly 2x end-to-end speedup from warm remote caching when the cache is
helping. If CI later executes only the affected subset of targets (for example,
via `bazel-diff`), additional end-to-end gains may be possible because both
executed work and the remaining cacheable surface area shrink. That said, the
incremental benefit may not scale linearly if the reduced workload becomes more
dominated by setup costs, startup overhead, or a smaller number of expensive
cache misses.

## What a future session should read first
Before changing anything, read:
- `plans/bazel-remote-cache-benchmark-handoff.md`
- `scripts/benchmark_bazel_remote_cache.sh`
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

## Current findings from the benchmark harness
The benchmark harness now supports the workflow shape actually used in CI:
- build-only cache matrix
- test-only cache matrix
- empty impacted-target comparison
- non-empty impacted-target comparison
- representative latency measurement and toxiproxy injection for both HTTP and gRPC

### Current benchmark/task state
The benchmark harness and Taskfile are intentionally split so future sessions do
not have to rediscover the operational shape:

- `task bazel-benchmark-remote-cache`
  - best-case empty impacted-set comparison for
    `test //... -- -//graft/...`
- `task bazel-benchmark-remote-cache-builds`
  - build-only cache matrix for:
    - `build //main:avalanchego`
    - `build --config=race //main:avalanchego`
- `task bazel-benchmark-remote-cache-tests`
  - full test cache matrix for:
    - `test //... -- -//graft/...`
- `task bazel-benchmark-remote-cache-impacted-nonempty`
  - warm-cache-vs-impacted comparison for a guaranteed non-empty changed set
- `task bazel-benchmark-remote-cache-ids-test`
  - narrow smoke validation for `//ids:ids_test`

This split exists because combining all measurements into one benchmark job made
CI runtime unreasonable for exploratory use.

### CI/runtime lesson from this session
A key operational finding is that the benchmark questions should not be forced
into one PR job.

Practical implication:
- keep the empty-set comparison focused
- collect build and test cache matrices separately
- keep the observational benchmark non-required

### Startup/readiness and diagnostics work already completed
Two benchmark-harness issues were found and fixed during this work:

1. daemon readiness handling
- previous startup logic could block forever waiting on healthy background
  daemons that were still starting
- current logic polls readiness for bounded time before failing

2. failure observability
- benchmark artifacts now upload `summary.log` plus inner diagnostic logs and
  `metadata.txt`
- this made it possible to distinguish real harness bugs from runner issues

### Current CI results to preserve
Successful run: `27788833041`

#### Build matrix
##### linux-amd64
- setup fetch: `48.829s`
- `build //main:avalanchego`
  - no cache: `174.391s`
  - HTTP cold: `183.434s`
  - HTTP warm: `52.415s`
  - gRPC cold: `186.452s`
  - gRPC warm: `55.441s`
- `build --config=race //main:avalanchego`
  - no cache: `169.355s`
  - HTTP cold: `179.309s`
  - HTTP warm: `51.438s`
  - gRPC cold: `182.402s`
  - gRPC warm: `54.472s`

##### darwin-arm64
- setup fetch: `50.180s`
- `build //main:avalanchego`
  - no cache: `143.606s`
  - HTTP cold: `162.529s`
  - HTTP warm: `46.237s`
  - gRPC cold: `153.697s`
  - gRPC warm: `45.325s`
- `build --config=race //main:avalanchego`
  - no cache: `143.064s`
  - HTTP cold: `155.085s`
  - HTTP warm: `45.520s`
  - gRPC cold: `159.995s`
  - gRPC warm: `45.982s`

#### Test matrix
##### linux-amd64
- setup fetch: `61.040s`
- no cache: `696.405s`
- HTTP cold: `804.939s`
- HTTP warm: `155.860s`
- gRPC cold: `800.068s`
- gRPC warm: `174.868s`

##### darwin-arm64
- setup fetch: `73.915s`
- no cache: `817.402s`
- HTTP cold: `821.140s`
- HTTP warm: `145.244s`
- gRPC cold: `841.446s`
- gRPC warm: `176.204s`

#### Empty impacted-target comparison
##### linux-amd64
- setup fetch: `54.958s`
- HTTP seed: `742.119s`
- HTTP warm: `84.632s`
- impacted selector: `69.433s`
- impacted execution: `0.000s`
- impacted total: `69.433s`
- impacted targets: `0`

##### darwin-arm64
- setup fetch: `82.606s`
- HTTP seed: `798.590s`
- HTTP warm: `81.958s`
- impacted selector: `82.114s`
- impacted execution: `0.000s`
- impacted total: `82.114s`
- impacted targets: `0`

#### Non-empty impacted-target comparison
An earlier catastrophic result here was caused by a real harness bug: impacted
execution was using a dead remote-cache endpoint because `bazel-remote` had
been stopped too early. That bug is fixed.

##### linux-amd64
- setup fetch: `57.016s`
- HTTP seed: `738.090s`
- HTTP warm: `74.683s`
- impacted selector: `65.482s`
- impacted execution: `54.462s`
- impacted total: `119.944s`
- impacted targets: `187`

##### darwin-arm64
- setup fetch: `77.287s`
- HTTP seed: `896.777s`
- HTTP warm: `150.824s`
- impacted selector: `89.772s`
- impacted execution: `122.029s`
- impacted total: `211.801s`
- impacted targets: `187`

### Current conclusions
For the current GitHub Actions workflow shape:
- warm remote cache is the dominant win
- impacted-target selection adds little incremental value on top of warm cache
- empty impacted-target selection is only modestly better on Linux and roughly
  break-even on macOS
- non-empty impacted execution is slower than a warm full-scope rerun on both
  tested platforms
- the main value here is therefore the remote cache itself, not impacted-target
  execution inside an already-created static job

### Architecture caveat
This conclusion is specific to static-job GitHub Actions, where impacted-target
selection competes only inside jobs that already exist.

A dynamic CI system that can avoid creating jobs at all could still benefit more
from early impact analysis.

## Optional follow-up only if needed
This work is basically complete for the GitHub Actions question on this branch.
Only continue if one of these becomes important:
- run the same benchmark harness on a baseline branch for a like-for-like
  comparison against Firewood-from-source
- revisit impact analysis for a dynamic CI system where it can skip jobs before
  they start

## Open questions worth keeping in mind
- Is the current hard-coded setup fetch union the right long-term shape, or
  should the harness eventually derive it mechanically once the benchmark model
  stabilizes?
- Should the observational benchmark remain non-required forever, or become a
  required informational job later?
- The benchmark currently measures representative latency against a public AWS
  endpoint. If a real remote cache deployment exists later, should the latency
  model be recalibrated against that real service?
