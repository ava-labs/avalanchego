# Firewood Metrics

Firewood provides comprehensive metrics for monitoring database performance, resource utilization, and operational characteristics. Metrics are defined internally without a `firewood.` prefix; the prefix is added only at export time for consistency in dashboards.

**Export Behavior**:

- **FFI/Go layer**: The custom HTTP renderer converts dots to underscores for Prometheus compatibility (no prefix added)
- **Benchmark/Prometheus**: The scrape job applies a relabel rule to prefix metric names with `firewood_` (see benchmark/setup-scripts/install-grafana.sh)
- **Prometheus queries**: Use underscore names (e.g., `firewood_proposal_commit`) when scraped via the benchmark Prometheus configuration

## Enabling Metrics

Metrics are available when Firewood is built with the `metrics` feature. By default, metrics collection is enabled in the library but needs to be explicitly started in applications.

**Important**: Only one metrics instance can be created per process. Attempting to initialize metrics multiple times will result in an error.

### For Rust Applications

Metrics are automatically registered when the instrumented code paths are executed. To expose metrics via HTTP:

```rust
use metrics_exporter_prometheus::PrometheusBuilder;

// Set up Prometheus exporter on port 9000
PrometheusBuilder::new()
    .install()
    .expect("failed to install Prometheus recorder");
```

### For FFI/Go Applications

In the Go FFI layer, metrics must be explicitly enabled:

```go
import "github.com/ava-labs/firewood-go-ethhash/ffi"

// Option 1: Start metrics with HTTP exporter on a specific port
ffi.StartMetricsWithExporter(9000)

// Option 2: Start metrics without exporter (use Gatherer to access)
ffi.StartMetrics()

// Retrieve metrics programmatically
gatherer := ffi.Gatherer{}
metrics, err := gatherer.Gather()
```

See the [FFI README](ffi/README.md) for more details on FFI metrics configuration.

## Available Metrics

### Database Operations

#### Proposal Metrics

- **`proposals`** (counter)
  - Description: Total number of proposals created
  - Use: Track proposal creation rate and throughput

- **`proposal.create`** (counter with `success` label)
  - Description: Count of proposal creation operations
  - Labels: `success=true|false`
  - Use: Monitor proposal creation success rate

- **`proposal.create_ms`** (counter with `success` label)
  - Description: Time spent creating proposals in milliseconds
  - Labels: `success=true|false`
  - Use: Track proposal creation latency

- **`proposal.commit`** (counter with `success` label)
  - Description: Count of proposal commit operations
  - Labels: `success=true|false`
  - Use: Monitor commit success rate

- **`proposal.commit_ms`** (counter with `success` label)
  - Description: Time spent committing proposals in milliseconds
  - Labels: `success=true|false`
  - Use: Track commit latency and identify slow commits

#### Revision Management

- **`active_revisions`** (gauge)
  - Description: Current number of active revisions in memory
  - Use: Monitor memory usage and revision retention

- **`max_revisions`** (gauge)
  - Description: Maximum number of revisions configured
  - Use: Track configuration setting

### Merkle Trie Operations

#### Insert Operations

- **`insert`** (counter with `merkle` label)
  - Description: Count of insert operations by type
  - Labels: `merkle=update|above|below|split`
    - `update`: Value updated at existing key
    - `above`: New node inserted above existing node
    - `below`: New node inserted below existing node
    - `split`: Node split during insertion
  - Use: Understand insert patterns and trie structure evolution

#### Remove Operations

- **`remove`** (counter with `prefix` and `result` labels)
  - Description: Count of remove operations
  - Labels:
    - `prefix=true|false`: Whether operation is prefix-based removal
    - `result=success|nonexistent`: Whether key(s) were found
  - Use: Track deletion patterns and key existence

### Storage and I/O Metrics

#### Node Reading

- **`read_node`** (counter with `from` label)
  - Description: Count of node reads by source
  - Labels: `from=file|memory`
  - Use: Monitor read patterns and storage layer usage

#### Cache Performance

- **`cache.node`** (counter with `mode` and `type` labels)
  - Description: Node cache hit/miss statistics
  - Labels:
    - `mode`: Read operation mode
    - `type=hit|miss`: Cache hit or miss
  - Use: Evaluate cache effectiveness for nodes

- **`cache.freelist`** (counter with `type` label)
  - Description: Free list cache hit/miss statistics
  - Labels: `type=hit|miss`
  - Use: Monitor free list cache efficiency

- **`cache.freelist.size`** (gauge)
  - Description: Current number of entries in the freelist cache
  - Use: Track actual cache utilization vs configured capacity; helps diagnose low hit rates by showing if cache is filling up

#### I/O Operations

- **`io.read`** (counter)
  - Description: Total number of I/O read operations
  - Use: Track I/O operation count

- **`io.read_ms`** (counter)
  - Description: Total time spent in I/O reads in milliseconds
  - Use: Identify I/O bottlenecks and disk performance issues

#### Node Persistence

- **`flush_nodes`** (counter)
  - Description: Cumulative time spent flushing nodes to disk in milliseconds (counter incremented by flush duration)
  - Use: Monitor flush performance and identify slow disk writes; calculate average flush time using rate()

### Memory Management

#### Space Allocation

- **`space.reused`** (counter with `index` label)
  - Description: Bytes reused from free list
  - Labels: `index`: Size index of allocated area
  - Use: Track memory reuse efficiency

- **`space.from_end`** (counter with `index` label)
  - Description: Bytes allocated from end of nodestore when free list was insufficient
  - Labels: `index`: Size index of allocated area
  - Use: Track database growth and free list effectiveness

- **`space.freed`** (counter with `index` label)
  - Description: Bytes freed back to free list
  - Labels: `index`: Size index of freed area
  - Use: Monitor memory reclamation

#### Node Management

- **`delete_node`** (counter with `index` label)
  - Description: Count of nodes deleted
  - Labels: `index`: Size index of deleted node
  - Use: Track node deletion patterns

-#### Ring Buffer

- **`ring.full`** (counter)
  - Description: Count of times the ring buffer became full during node flushing
  - Use: Identify backpressure in node persistence pipeline

- **`ring.eagain_write_retry`** (counter)
  - Description: Amount of io-uring write entries that have been re-submitted due to `EAGAIN` io error.
  - Use: identify interrupted writes

- **`ring.partial_write_retry`** (counter)
  - Description: Amount of io-uring write entries that have been re-submitted due to partial writes.
  - Use: identify partial writes

### FFI Layer Metrics

These metrics are specific to the Foreign Function Interface (Go) layer:

#### Batch Operations

- **`ffi.batch`** (counter)
  - Description: Count of batch operations completed
  - Use: Track FFI batch throughput

- **`ffi.batch_ms`** (counter)
  - Description: Time spent processing batches in milliseconds
  - Use: Monitor FFI batch latency

#### Proposal Operations

- **`ffi.propose`** (counter)
  - Description: Count of proposal operations via FFI
  - Use: Track FFI proposal throughput

- **`ffi.propose_ms`** (counter)
  - Description: Time spent creating proposals via FFI in milliseconds
  - Use: Monitor FFI proposal latency

#### Commit Operations

- **`ffi.commit`** (counter)
  - Description: Count of commit operations via FFI
  - Use: Track FFI commit throughput

- **`ffi.commit_ms`** (counter)
  - Description: Time spent committing via FFI in milliseconds
  - Use: Monitor FFI commit latency

#### View Caching

- **`ffi.cached_view.hit`** (counter)
  - Description: Count of cached view hits
  - Use: Monitor view cache effectiveness

- **`ffi.cached_view.miss`** (counter)
  - Description: Count of cached view misses
  - Use: Monitor view cache effectiveness

## Interpreting Metrics

### Performance Monitoring

1. **Latency Tracking**: The `*_ms` metrics track operation durations. Monitor these for:
   - Sudden increases indicating performance degradation
   - Baseline establishment for SLA monitoring
   - Correlation with system load

2. **Throughput Monitoring**: Counter metrics without `_ms` suffix track operation counts:
   - Rate of change indicates throughput
   - Compare with expected load patterns
   - Identify anomalies in operation rates

### Resource Utilization

1. **Cache Efficiency**:
   - Calculate hit rate: `cache.hit / (cache.hit + cache.miss)`
   - Target: >90% for node cache, >80% for free list cache
   - Low hit rates may indicate insufficient cache size

2. **Memory Management**:
   - Monitor `space.reused` vs `space.from_end` ratio
   - High `space.from_end` indicates database growth
   - High `space.wasted` suggests fragmentation issues

3. **Active Revisions**:
   - `active_revisions` approaching `max_revisions` triggers cleanup
   - Sustained high values may indicate memory pressure

### Debugging

1. **Failed Operations**:
   - Check metrics with `success=false` label
   - Correlate with error logs for root cause analysis

2. **Ring Buffer Backpressure**:

- `firewood_ring_full` (exported) counter increasing indicates persistence bottleneck
- May require tuning of flush parameters or disk subsystem

1. **Insert/Remove Patterns**:

   - `firewood.insert` labels show trie structure evolution
   - High `split` counts indicate complex key distributions
   - Remove `nonexistent` suggests application-level issues

## Example Monitoring Queries

For Prometheus-based monitoring (note: metric names use underscores in queries):

```promql
# Average commit latency over 5 minutes
rate(firewood_proposal_commit_ms[5m]) / rate(firewood_proposal_commit[5m])

# Cache hit rate
sum(rate(firewood_cache_node{type="hit"}[5m])) /
sum(rate(firewood_cache_node[5m]))

# Database growth rate (bytes/sec)
rate(firewood_space_from_end[5m])

# Failed commit ratio
rate(firewood_proposal_commit{success="false"}[5m]) /
rate(firewood_proposal_commit[5m])
```

## Performance Tracking

Firewood tracks its performance over time by running [C-Chain reexecution benchmarks](https://github.com/ava-labs/avalanchego/blob/master/tests/reexecute/c/README.md) in AvalancheGo. These benchmarks re-execute historical mainnet C-Chain blocks against a state snapshot, measuring throughput in mgas/s (million gas per second).

This allows us to:

- Monitor performance across commits and releases
- Catch performance regressions early
- Validate optimizations against real-world blockchain workloads

Performance data is collected via the `Track Performance` workflow and published to GitHub Pages.

### Running Benchmarks from GitHub UI

The easiest way to trigger a benchmark is via the GitHub Actions UI:

1. Go to [Actions → Track Performance](https://github.com/ava-labs/firewood/actions/workflows/track-performance.yml)
2. Click "Run workflow"
3. Select parameters from the dropdowns (task, runner) or enter custom values
4. Click "Run workflow"

### Triggering Benchmarks via CLI

Benchmarks run on AvalancheGo's self-hosted runners, not locally. This enables end-to-end integration testing where:

- Firewood team can benchmark changes against the full AvalancheGo stack
- AvalancheGo team can iterate on their Firewood integration

```mermaid
sequenceDiagram
    participant F as Firewood
    participant A as AvalancheGo
    participant G as GitHub Pages

    F->>A: 1. trigger workflow
    A->>A: 2. run benchmark
    A-->>F: 3. download results
    F->>G: 4. publish
```

The CLI commands trigger the remote workflow, wait for completion, and download the results.

```bash
nix run ./ffi#gh -- auth login
export GH_TOKEN=$(gh auth token)

# Predefined test
just bench-cchain test=firewood-101-250k

# With specific Firewood version
FIREWOOD_REF=v0.1.0 just bench-cchain test=firewood-33m-40m

# Custom block range
START_BLOCK=101 END_BLOCK=250000 \
  BLOCK_DIR_SRC=cchain-mainnet-blocks-1m-ldb \
  CURRENT_STATE_DIR_SRC=cchain-current-state-firewood-100 \
  just bench-cchain
```

**Command:**

```bash
just bench-cchain [test]
```

Triggers Firewood's `track-performance.yml` workflow, which orchestrates the AvalancheGo benchmark. The command polls for the workflow run and watches progress in terminal.

> **Note:** Changes must be pushed to the remote branch for the workflow to use them. By default, the workflow builds Firewood from the current commit. To benchmark a specific version (e.g., a release tag), set `FIREWOOD_REF` explicitly.

**Environment variables and options:** See [scripts/bench-cchain-reexecution.sh](scripts/bench-cchain-reexecution.sh) or run `./scripts/bench-cchain-reexecution.sh help`.

**Tests and runners** are defined in AvalancheGo:

- [Available tests](https://github.com/ava-labs/avalanchego/blob/master/scripts/benchmark_cchain_range.sh)
- [C-Chain benchmark docs](https://github.com/ava-labs/avalanchego/blob/master/tests/reexecute/c/README.md)

### Viewing Results

Results are published to GitHub Pages via [github-action-benchmark](https://github.com/benchmark-action/github-action-benchmark).

**Graph location:**

- [Main branch trends](https://ava-labs.github.io/firewood/bench/) — official benchmark history
- [Feature branch trends](https://ava-labs.github.io/firewood/dev/bench/) — experimental runs

**Downloading raw data:**

- Click "Download data as JSON" at the bottom of any benchmark page
- Or view raw data directly: [benchmark-data/bench/data.js](https://github.com/ava-labs/firewood/blob/benchmark-data/bench/data.js)
