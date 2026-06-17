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
- workflow: `.github/workflows/bazel-ci.yml`

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

## Next investigation: representative-latency HTTP vs gRPC cache comparison
The next step is to evolve the generalized benchmark from a simple
“remote cache helps or not” experiment into a protocol and latency
comparison that better reflects the intended deployment shape.

## Next planned extension: partition build-vs-test minimization comparisons
The next benchmark change should explicitly partition **build workloads** from
**test workloads** instead of forcing one uniform comparison matrix across all
Bazel commands.

### Why partition the benchmark
The current generalized harness treats each benchmark entry as just “a Bazel
command,” but the decision space is now different for builds and tests:

- **Build workloads** (for example `build //main:avalanchego`) do not benefit
  meaningfully from impacted-target reduction once the job is required. If CI
  needs that binary, the build still has to happen in order to populate or
  reuse action-cache entries. For builds, the important question remains how
  much remote caching helps, and whether HTTP vs gRPC matters.
- **Test workloads** can benefit from impacted-target selection because the
  selected set may be empty or very small. For tests, the important comparison
  is no longer only cache/no-cache/protocol, but also whether computing
  impacted targets and possibly skipping test execution beats simply running the
  full-scope test command against a warm remote cache.

This means the benchmark should stop pretending that “impacted targets” is a
symmetric dimension for both builds and tests. The likely product conclusion is
that remote caching is the main lever for builds, while impacted-target
selection is only plausibly worthwhile on the test side.

### Benchmark shape to add next
The next extension should keep the existing generalized script but add
**workload-mode-aware benchmark entries**.

#### Build entries
Build-mode entries should keep the current full remote-cache matrix:
- no remote cache
- HTTP remote cache, cold
- HTTP remote cache, warm
- gRPC remote cache, cold
- gRPC remote cache, warm

This should continue to cover at least:
- `build //main:avalanchego`
- `build --config=race //main:avalanchego`

No impacted-target comparison should be added for build entries.

#### Test entries
Test-mode entries should use a smaller matrix focused on the decision that now
matters for tests:
- no remote cache
- HTTP remote cache, cold
- HTTP remote cache, warm
- impacted-target mode

For the first iteration, **skip gRPC test benchmarking**. gRPC remains useful
on the build side where protocol comparison is more interesting, but adding it
for tests would increase script complexity without answering the main product
question.

The initial test entry should be the existing main-module unit-test workload:
- `test //... -- -//graft/...`

### What “impacted-target mode” should measure
For each test-mode benchmark entry, the harness should measure a fourth case
that uses the same test scope as the full benchmark command but executes via
impacted-target selection.

That impacted case should record:
1. **selector time** — time to compute the impacted target manifest
2. **selected target count** — how many test labels were selected
3. **execution time** — zero if the manifest is empty; otherwise time to run
   `bazel test` on the selected labels
4. **total impacted time** — selector time + execution time

The most important comparison is:
- **full-scope warm HTTP cache**
- vs **impacted-target total time**

This is the real decision point for test CI minimization:
- if impacted-target total is materially lower than warm-cache full-scope test
  execution, test selection complexity may be justified
- if the numbers are close, warm remote caching may already provide most of the
  practical value without selective execution

### Important fairness requirement
The impacted-target comparison should use the same CI-style isolation model as
other measured runs:
- fresh `--output_base`
- shared setup-side dependency caches only (`repository_cache` and shared
  Gazelle `GOMODCACHE`)
- no reuse of prior local Bazel action/output state

That keeps the comparison honest:
- **fresh worker + warm HTTP remote cache on the full test scope**
- versus
- **fresh worker + impacted-target selection for the same test scope**

The benchmark should not let impacted-target mode cheat by reusing warmed local
Bazel state that the full-cache benchmark cases do not get.

### Configuration direction
The script interface should evolve from “one uniform list of benchmark args” to
“typed benchmark entries” or equivalent workload-specific flags.

The current Taskfile should stay as the configuration layer for this script, so
any new interface should remain practical to express from Task definitions.

#### Proposed first-pass CLI shape
The simplest likely interface is to keep one shared setup command and split the
benchmark entrypoints by workload type:

- `--build-benchmark-args '<bazel build ...>'`
- `--test-benchmark-args '<bazel test ...>'`
- `--test-impacted-scope '<scope>'`
- `--test-impacted-range '<range>'` or environment-backed equivalent

Intended semantics:
- every `--build-benchmark-args` entry runs the existing build-style matrix:
  no-cache / HTTP cold / HTTP warm / gRPC cold / gRPC warm
- every `--test-benchmark-args` entry runs the test-style matrix:
  no-cache / HTTP cold / HTTP warm / impacted-target
- `--test-impacted-scope` values define the scope universe used by the
  impacted-target selector for all configured test entries in the first
  iteration
- `--test-impacted-range` defines the git diff range used for impacted-target
  selection; if this is omitted, implementation should likely support the same
  environment-backed base-SHA model already used by the impacted-target helper
  scripts

This first pass intentionally supports one shared impacted-target scope set for
all configured test entries. That is sufficient for the initial use case with a
single test workload (`test //... -- -//graft/...`) and keeps the bash parsing
manageable.

If later benchmark coverage needs per-test-entry selector scopes, the interface
can graduate to a more structured entry format after the first iteration proves
useful.

#### Proposed first vertical slice
To minimize implementation risk, the first change should wire up only:
- existing build entries unchanged:
  - `build //main:avalanchego`
  - `build --config=race //main:avalanchego`
- one test entry with impacted-target comparison:
  - `test //... -- -//graft/...`
- one matching impacted-target scope set:
  - `//...`
  - `-//graft/...`

That keeps the setup fetch union unchanged for the first pass and lets the new
logic prove itself on one test workload before expanding to more selective test
scopes.

#### Proposed script behavior for test entries
For each test entry, after the no-cache / HTTP cold / HTTP warm runs:
1. measure selector start/end time
2. invoke the existing impacted-target machinery for the configured scope set
3. record the selected target count
4. if the manifest is empty:
   - record execution time as `0`
   - record total impacted-target time as selector time only
5. if the manifest is non-empty:
   - run `bazel test` on exactly the selected labels with a fresh output base
   - record execution time and total impacted-target time

The script output for test entries should therefore gain explicit lines for:
- `impacted-selector`
- `impacted-execution`
- `impacted-total`
- `impacted-target-count`

#### Proposed implementation constraints
The first implementation should:
- reuse the existing helper scripts/tooling for impacted-target computation
  rather than duplicating selector logic inside the benchmark script
- preserve current build-entry result formatting and pass/fail semantics
- avoid introducing impacted-target logic into build entries just for symmetry
- keep gRPC out of the test matrix for now

That yields a small, reviewable change set while still producing the comparison
needed to decide whether test-side impacted-target pruning is worth the added
complexity over warm remote caching.

### Validation criteria for this next step
A successful implementation of this extension should:
- preserve the current build benchmark behavior and protocol comparison
- add at least one test-mode benchmark entry that measures impacted-target mode
  alongside no-cache / HTTP cold / HTTP warm
- ensure the impacted-target selection uses the same scope as the corresponding
  full test benchmark command
- report selector time, selected target count, execution time, and total
  impacted-target time
- make it easy to compare **warm HTTP full-scope test execution** against
  **impacted-target execution** on the same CI runner class

### Decision this extension should inform
This extension is meant to answer a narrower but more actionable question than
“does remote cache help?”

It should answer:
- for test workloads, is it better to compute impacted targets and sometimes
  skip almost all work, or to rely on warm remote caching for the full test
  scope?
- for build workloads, does the inability to shrink the required top-level
  target set mean remote cache should remain the only optimization under
  consideration?

That answer should clarify whether selective-target complexity is justified only
for tests, while remote cache remains the primary optimization for builds.

## Current findings from the impacted-target benchmark extension
The exploratory benchmark was narrowed to the **best-case** question for tests:

- populate a warm HTTP remote cache for `test //... -- -//graft/...`
- rerun the same full-scope test command against that warm cache
- compare that with an explicitly **empty** impacted-target range, where the
  selector should return zero tests

This is the comparison that matters when remote cache is assumed to exist
anyway and impacted targets is being evaluated only as an additional
optimization for the “run nothing” case.

### Why only the best case matters here
For this line of investigation, the important product question is not whether
impacted targets helps in every case. Remote cache is already assumed to be the
baseline optimization. Impacted targets is only interesting as an extra layer
when the correct answer is that **no tests should run**.

So the meaningful comparison is:
- **warm full-scope Bazel test invocation**
- versus
- **impacted-target selection returning an empty set**

If some tests actually need to run, remote cache is still required and the
selective-target path has a much higher bar to justify its extra complexity.

### Best-case benchmark results
Observed CI results for the best-case empty-manifest comparison:

#### linux-amd64
- setup fetch: `56.965s`
- warm-cache seed run: `779.380s`
- warm-cache rerun: `95.668s`
- impacted selector total: `71.491s`
- impacted execution: `0.000s`
- impacted selected targets: `0`

Interpretation:
- empty impacted-target selection beat warm full-scope cache reuse by about
  `24s`
- this is directionally meaningful but not dramatic; on Linux the best-case win
  is roughly in the 20–25% range

#### darwin-arm64
- setup fetch: `100.579s`
- warm-cache seed run: `1068.434s`
- warm-cache rerun: `187.155s`
- impacted selector total: `99.829s`
- impacted execution: `0.000s`
- impacted selected targets: `0`

Interpretation:
- empty impacted-target selection beat warm full-scope cache reuse by about
  `87s`
- the relative win is much larger on macOS because the warm full-scope Bazel
  path is substantially more expensive there than the selector path

### Non-best-case observation
An earlier run against a **non-empty** impacted set selected a large number of
main-module tests and showed the selective path performing worse than the warm
full-scope cached run. That is consistent with the intended interpretation:
- impacted targets is not attractive when many tests still need to run
- the only case that really matters for deciding whether to add this complexity
  is the empty-set best case

### Practical takeaway so far
Current takeaway from the exploratory benchmark:
- warm remote cache already delivers large value on CI-style Bazel workloads
- impacted-target selection appears to provide only a **modest** extra win on
  Linux in the empty-set best case, though the macOS result is more favorable
- this suggests selective-target complexity is not obviously compelling as a
  universal in-job optimization over warm cache alone, at least in the current
  GitHub Actions model

### CI-architecture implication worth remembering
This benchmark was run in a GitHub Actions workflow where jobs are declared
statically, so the selector only competes with Bazel **inside an already
created job**.

A future dynamic-pipeline CI system (for example Buildkite) could use the same
kind of change/impact analysis earlier, before most jobs are created. In that
model, impacted-target selection could avoid per-job startup and setup costs in
addition to Bazel execution costs. That likely makes change-based selection more
attractive there than it appears in static-job GitHub Actions.

Treat that as an architectural implication to revisit later rather than as a
conclusion already proven by this benchmark.

### What to preserve for future sessions
The successful outcome of this exploratory work was learning that the benchmark
must distinguish between:
- the large already-demonstrated value of warm remote caching itself
- the narrower incremental value of impacted-target selection on top of that

Future follow-up should preserve and extend actual numbers for:
- setup fetch cost
- warm-cache seed cost
- warm-cache rerun cost
- empty impacted-target selector cost
- any later repeated measurements needed to understand variance across runners

If this work resumes later, the next likely need is not more architecture
guessing but **more collected statistics** comparing caching versus not caching
and, separately, the empty-set incremental benefit of impacted-target selection
on top of caching.

## Current benchmark/task state
The benchmark harness and Taskfile are now intentionally split so future
sessions do not have to rediscover the current operational shape:

- `task bazel-benchmark-remote-cache`
  - currently runs the **best-case test-only benchmark**
  - scope: `test //... -- -//graft/...`
  - comparison: warm cached full-scope rerun vs explicitly empty impacted set
- `task bazel-benchmark-remote-cache-builds`
  - currently runs the **build-only remote-cache matrix**
  - workloads:
    - `build //main:avalanchego`
    - `build --config=race //main:avalanchego`
- `task bazel-benchmark-remote-cache-ids-test`
  - remains the narrow smoke task for quickly validating benchmark behavior on
    `//ids:ids_test`

This split exists because combining the full build matrix and the full test
matrix into one PR benchmark job made CI runtime unreasonable for exploratory
use. Future sessions should preserve that lesson unless they intentionally move
these measurements into separate workflows, manual jobs, or another CI system.

## CI/runtime lesson from this session
A key operational finding from this work is that the benchmark questions should
not all be forced into one PR job.

What happened:
- the benchmark initially expanded from build-only measurements into a combined
  build+test benchmark
- that produced hour-scale runtime and made the PR job look hung
- the underlying problem was not the impacted-target idea itself; it was trying
  to collect too many long-running measurements in a single observational job

Practical implication:
- keep the **best-case empty-set test comparison** small and focused
- collect the **full cache matrix** for builds and tests separately when needed
- treat broad statistics gathering as a deliberate follow-up exercise, not as
  something that should automatically run inside one default PR benchmark job

## Startup/readiness bug already found and fixed
A Linux CI timeout during this work turned out not to be a mysterious Bazel
hang, but a benchmark-script bug in daemon startup handling.

Previous bug:
- `start_bazel_remote` and `start_toxiproxy` launched long-lived background
  daemons
- after a single fixed sleep, the script called `wait` when readiness checks
  were not yet passing
- if a daemon was healthy but still starting, the script could block forever on
  `wait` for a process that was not supposed to exit

Current fix:
- both helpers now poll readiness for a bounded period
- `bazel-remote` readiness is checked via HTTP
- `toxiproxy` readiness is checked via `toxiproxy-cli list`
- the helpers only kill/wait when startup genuinely fails

Future sessions should treat that bug as already understood. If a benchmark job
appears stuck in similar early startup phases again, inspect readiness polling
or the underlying services — not the old single-sleep-plus-wait behavior.

## Full measurement set still desired
The best-case empty-set comparison above answered one narrow exploratory
question, but it is **not** the complete dataset needed for documenting the
overall tradeoffs and recommending a direction.

The broader numbers still needed are:

### Remote-cache matrix for real Bazel workloads
For representative CI workloads, collect:
- no cache
- HTTP cold
- HTTP warm
- gRPC cold
- gRPC warm

This should cover both:
- build workloads
- test workloads

That dataset is what supports statements about:
- the total value of remote caching itself
- the difference between cold and warm cache behavior
- whether HTTP is operationally sufficient
- whether gRPC is worth the extra requirement/complexity

### Impacted-target comparison layered on top of test workloads
For test workloads, collect impacted-target measurements alongside the cache
matrix, including at least:
- impacted selector time
- impacted execution time
- impacted total time
- impacted selected target count

And preserve at least two selector scenarios:
- **empty impacted set** — the best-case “run nothing” decision path
- **non-empty impacted set** — so later writeups can show where the selective
  path stops being competitive with warm cache

## Recommended next steps for future data collection
To support later documentation and recommendation writing, the next session on
this work should collect and organize a fuller table of numbers rather than
continuing to reshape the benchmark architecture.

### 1. Re-run the full remote-cache matrix for builds
Collect CI numbers for build workloads with:
- no cache
- HTTP cold
- HTTP warm
- gRPC cold
- gRPC warm

Suggested workloads:
- `build //main:avalanchego`
- `build --config=race //main:avalanchego`

### 2. Re-run the full remote-cache matrix for tests
Collect CI numbers for the representative test workload with:
- no cache
- HTTP cold
- HTTP warm
- gRPC cold
- gRPC warm

Suggested workload:
- `test //... -- -//graft/...`

This may need to run in a separate observational task/workflow from the current
best-case benchmark if runtime becomes too large for one CI job.

### 3. Preserve the best-case empty impacted-target comparison
Keep collecting the empty-set comparison for the same test scope:
- warm cached full-scope rerun
- empty impacted-target selector path

This is the key number for evaluating whether impacted targets is worth keeping
as an incremental optimization on top of remote cache.

### 4. Collect at least one non-empty impacted-target comparison
Use a known range or fixture that selects a non-trivial but bounded test set so
future documentation can show the contrast between:
- empty impacted set
- non-empty impacted set
- warm cached full-scope execution

### 5. Summarize results in a durable comparison table
Future writeups should be able to cite one compact table containing, per runner
class and workload:
- setup fetch
- no cache
- HTTP cold
- HTTP warm
- gRPC cold
- gRPC warm
- impacted selector
- impacted execution
- impacted total
- impacted target count

That table is the artifact needed for documenting:
- what remote caching buys on its own
- how much more impacted targets buys, if anything
- whether the additional selective-target complexity is justified

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

Steps 1 and 2 are now complete.

## Recent implementation results and remaining follow-up
The benchmarked HTTP and gRPC cache traffic now both route through the
validated proxy path in `scripts/benchmark_bazel_remote_cache.sh`, and the
narrow `ids_test` smoke entrypoint remains just a parameterized call into that
same script.

Additional findings to carry into the next session:
- restoring shared `GOMODCACHE` support was consistent with the original intent
  of commit `b1c2624d7a` (`Share Gazelle module cache across benchmark runs`)
- the later loss of those flags appears to have been an accidental stale-base
  overwrite, not a documented intentional reversal
- setup-side dependency caches should be reusable across repeated local
  benchmark invocations for faster iteration, and that reuse now defaults on
  for local runs; it must apply only to setup state (`repository_cache` and
  shared `GOMODCACHE`), not to the remote cache contents. Fresh setup caches
  should remain available via an explicit override for debugging.
- remote cache directories must remain isolated per benchmark command because
  the point of the benchmark is to measure the delta between no-cache, HTTP
  cold-warm, and gRPC cold-warm behavior; reusing remote cache state would
  invalidate the cold-run semantics
- `fetch --all` turned out to be the wrong setup command for this benchmark.
  With `--experimental_ui_debug_all_events`, Bazel showed that `fetch --all`
  was downloading many irrelevant transitive repos and cross-platform toolchain
  artifacts (for example, extra JDKs and KSP inputs) unrelated to the
  benchmarked jobs. That was the main reason local setup appeared to "stall"
  behind repeated `Computing main repo mapping:` lines.
- because this harness is still exploratory, the setup fetch is now hard-coded
  to the current union of benchmarked target patterns instead of being derived
  mechanically. Keep that union aligned with the benchmark command list until
  the harness stabilizes enough to justify derivation or replacement.
- documentation should be de-duplicated: `docs/bazel.md` should remain the
  canonical user-facing explanation of the Bazel benchmark model and shared
  `GOMODCACHE` requirement, while this handoff should record investigation
  status, regressions, decisions, and next steps and link back to the doc
- upgrading Gazelle to `0.51.3` stopped `bazel mod tidy` from re-adding local
  `go.work` workspace modules (`avalanchego`, `graft/coreth`, `graft/evm`,
  `graft/subnet-evm`) as generated `use_repo(...)` self-repos. Older Gazelle
  behavior had made `fetch --all` even noisier by traversing those local-path
  repos.
- after the Gazelle upgrade, Bazel resolves `rules_go@0.59.0` through the
  dependency graph while the root module still declares `rules_go@0.57.0`.
  That emits a direct-dependency mismatch warning during module operations and
  is a worthwhile follow-up for a later session.

## Follow-up step after proxied HTTP benchmarking
Once the existing HTTP benchmark traffic is routed through the validated proxy:
1. keep reusing the existing Bazel repository cache and shared Gazelle module
   cache during iteration so setup work does not dominate feedback time
2. enable the gRPC listener and add the parallel gRPC proxying path
3. expand the comparison matrix to:
   - no cache
   - HTTP cold
   - HTTP warm
   - gRPC cold
   - gRPC warm
4. confirm the benchmark still behaves sensibly under induced latency for both
   protocols

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
- inject that latency into benchmarked HTTP and gRPC cache traffic
- preserve the current no-cache / HTTP cold-warm / gRPC cold-warm comparison
  semantics while doing so
- re-validate the full task-configured generalized benchmark in a healthy
  environment now that the setup fetch scope is narrowed
- make it easy to compare protocol behavior under the same latency model

## Open questions worth keeping in mind
- Is the current hard-coded setup fetch union the right long-term shape, or
  should the harness eventually derive it mechanically once the benchmark model
  stabilizes?
- Should the observational benchmark remain non-required forever, or become a required informational job later?
- If CI runner networking remains flaky even after shared `GOMODCACHE`, should the benchmark artifact upload step become more failure-tolerant, or should the benchmark be temporarily scoped by platform?
