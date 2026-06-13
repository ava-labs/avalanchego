# Firewood Benchmarks

Firewood has two categories of benchmarks:

- **Macro benchmarks** — C-Chain re-execution against real mainnet state; measures end-to-end system throughput
- **Micro benchmarks** — synthetic workloads and criterion suites that exercise the Rust API directly, no AvalancheGo required

## What we have

| Benchmark | Use when | Environment |
| --- | --- | --- |
| C-Chain re-execution — GitHub Actions | Tracking performance over time, A/B between versions | Managed CI — scheduled, results published to GitHub Pages |
| C-Chain re-execution — fwdctl launch | Investigating regression, in-depth profiling, tuning | EC2 instance you provision — full root access |
| Rust criterion | Iterating on a specific operation locally; enforces pure-Rust API in CI | Local or CI (`benchmarks.yaml`) |
| Synthetic workloads | Testing Firewood API patterns without AvalancheGo | Local |

## Details

### C-Chain re-execution — GitHub Actions

**When:** tracking performance over time or comparing two versions — runs automatically on a daily schedule and on every trigger, no manual setup needed.

Part of CI — repeatable, versioned, and auditable. Runs on a fixed schedule
and on demand via
[`track-performance.yml`](.github/workflows/track-performance.yml). Each run
is isolated on a dedicated self-hosted runner, keeping variance low enough that
a meaningful difference reflects code, not infrastructure. Results accumulate
on GitHub Pages.

→ [Full guide](docs/cchain-reexecution.md)

### C-Chain re-execution — fwdctl launch

**When:** a GitHub Actions run surfaced a signal worth investigating, or when running reexecution tests expected to take longer than 24 hours — SSH into the instance, install any tooling, change code and rebuild freely.

Same workload as GitHub Actions, provisioned on demand on EC2. Full root access
— install `perf`, flamegraphs, or any tooling, change code and rebuild freely.
No CI queue, no constraints.

→ [Full guide](../fwdctl/README.launch.md)

### Rust criterion

**When:** changing a core area of Firewood — these benchmarks cover small critical sections and are the fastest way to detect performance problems before introducing the FFI and Go layers.

Criterion benchmarks live in `firewood/benches/` and `storage/benches/`. They
run in seconds with no external dependencies and also run in CI on every push to
`main`.

| Changed area | Benchmark file |
| --- | --- |
| Deferred persistence | `firewood/benches/defer_persist.rs` |
| Hashing | `firewood/benches/hashops.rs` |
| Node serialization / deserialization | `storage/benches/serializer.rs` |

```bash
cargo bench --features ethhash,logger
```

Benchmarks can also produce flamegraphs. See the header comment in
`firewood/benches/hashops.rs` for instructions; `--profile-time=5` is a good
starting point.

### Synthetic workloads

**When:** changing any core area of Firewood — these tests can help identify performance problems before introducing the FFI and Go layers.

A standalone Rust binary exercising the Firewood API directly with synthetic
trie patterns (tenkrandom, zipf, single). No AvalancheGo, no Go, no cloud.

→ [Workload specs](docs/synthetic-workloads.md)
