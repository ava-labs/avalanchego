# Firewood Metrics

Firewood provides comprehensive metrics for monitoring database performance, resource utilization, and operational characteristics. These metrics are built using the [Prometheus](https://prometheus.io/) format and can be exposed for collection by monitoring systems.

**Note**: Metric names in this documentation use dots (e.g., `firewood.proposal.commit`), but when exported to Prometheus, dots are automatically converted to underscores (e.g., `firewood_proposal_commit`) following Prometheus naming conventions.

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

- **`firewood.proposals`** (counter)
  - Description: Total number of proposals created
  - Use: Track proposal creation rate and throughput

- **`firewood.proposal.create`** (counter with `success` label)
  - Description: Count of proposal creation operations
  - Labels: `success=true|false`
  - Use: Monitor proposal creation success rate

- **`firewood.proposal.create_ms`** (counter with `success` label)
  - Description: Time spent creating proposals in milliseconds
  - Labels: `success=true|false`
  - Use: Track proposal creation latency

- **`firewood.proposal.commit`** (counter with `success` label)
  - Description: Count of proposal commit operations
  - Labels: `success=true|false`
  - Use: Monitor commit success rate

- **`firewood.proposal.commit_ms`** (counter with `success` label)
  - Description: Time spent committing proposals in milliseconds
  - Labels: `success=true|false`
  - Use: Track commit latency and identify slow commits

#### Revision Management

- **`firewood.active_revisions`** (gauge)
  - Description: Current number of active revisions in memory
  - Use: Monitor memory usage and revision retention

- **`firewood.max_revisions`** (gauge)
  - Description: Maximum number of revisions configured
  - Use: Track configuration setting

### Merkle Trie Operations

#### Insert Operations

- **`firewood.insert`** (counter with `merkle` label)
  - Description: Count of insert operations by type
  - Labels: `merkle=update|above|below|split`
    - `update`: Value updated at existing key
    - `above`: New node inserted above existing node
    - `below`: New node inserted below existing node
    - `split`: Node split during insertion
  - Use: Understand insert patterns and trie structure evolution

#### Remove Operations

- **`firewood.remove`** (counter with `prefix` and `result` labels)
  - Description: Count of remove operations
  - Labels:
    - `prefix=true|false`: Whether operation is prefix-based removal
    - `result=success|nonexistent`: Whether key(s) were found
  - Use: Track deletion patterns and key existence

### Storage and I/O Metrics

#### Node Reading

- **`firewood.read_node`** (counter with `from` label)
  - Description: Count of node reads by source
  - Labels: `from=file|memory`
  - Use: Monitor read patterns and storage layer usage

#### Cache Performance

- **`firewood.cache.node`** (counter with `mode` and `type` labels)
  - Description: Node cache hit/miss statistics
  - Labels:
    - `mode`: Read operation mode
    - `type=hit|miss`: Cache hit or miss
  - Use: Evaluate cache effectiveness for nodes

- **`firewood.cache.freelist`** (counter with `type` label)
  - Description: Free list cache hit/miss statistics
  - Labels: `type=hit|miss`
  - Use: Monitor free list cache efficiency

#### I/O Operations

- **`firewood.io.read`** (counter)
  - Description: Total number of I/O read operations
  - Use: Track I/O operation count

- **`firewood.io.read_ms`** (counter)
  - Description: Total time spent in I/O reads in milliseconds
  - Use: Identify I/O bottlenecks and disk performance issues

#### Node Persistence

- **`firewood.flush_nodes`** (counter)
  - Description: Cumulative time spent flushing nodes to disk in milliseconds (counter incremented by flush duration)
  - Use: Monitor flush performance and identify slow disk writes; calculate average flush time using rate()

### Memory Management

#### Space Allocation

- **`firewood.space.reused`** (counter with `index` label)
  - Description: Bytes reused from free list
  - Labels: `index`: Size index of allocated area
  - Use: Track memory reuse efficiency

- **`firewood.space.wasted`** (counter with `index` label)
  - Description: Bytes wasted when allocating from free list (allocated more than needed)
  - Labels: `index`: Size index of allocated area
  - Use: Monitor allocation efficiency and fragmentation

- **`firewood.space.from_end`** (counter with `index` label)
  - Description: Bytes allocated from end of nodestore when free list was insufficient
  - Labels: `index`: Size index of allocated area
  - Use: Track database growth and free list effectiveness

- **`firewood.space.freed`** (counter with `index` label)
  - Description: Bytes freed back to free list
  - Labels: `index`: Size index of freed area
  - Use: Monitor memory reclamation

#### Node Management

- **`firewood.delete_node`** (counter with `index` label)
  - Description: Count of nodes deleted
  - Labels: `index`: Size index of deleted node
  - Use: Track node deletion patterns

#### Ring Buffer

- **`ring.full`** (counter)
  - Description: Count of times the ring buffer became full during node flushing
  - Use: Identify backpressure in node persistence pipeline

### FFI Layer Metrics

These metrics are specific to the Foreign Function Interface (Go) layer:

#### Batch Operations

- **`firewood.ffi.batch`** (counter)
  - Description: Count of batch operations completed
  - Use: Track FFI batch throughput

- **`firewood.ffi.batch_ms`** (counter)
  - Description: Time spent processing batches in milliseconds
  - Use: Monitor FFI batch latency

#### Proposal Operations

- **`firewood.ffi.propose`** (counter)
  - Description: Count of proposal operations via FFI
  - Use: Track FFI proposal throughput

- **`firewood.ffi.propose_ms`** (counter)
  - Description: Time spent creating proposals via FFI in milliseconds
  - Use: Monitor FFI proposal latency

#### Commit Operations

- **`firewood.ffi.commit`** (counter)
  - Description: Count of commit operations via FFI
  - Use: Track FFI commit throughput

- **`firewood.ffi.commit_ms`** (counter)
  - Description: Time spent committing via FFI in milliseconds
  - Use: Monitor FFI commit latency

#### View Caching

- **`firewood.ffi.cached_view.hit`** (counter)
  - Description: Count of cached view hits
  - Use: Monitor view cache effectiveness

- **`firewood.ffi.cached_view.miss`** (counter)
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
   - `ring.full` counter increasing indicates persistence bottleneck
   - May require tuning of flush parameters or disk subsystem

3. **Insert/Remove Patterns**:
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
