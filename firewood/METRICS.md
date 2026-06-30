# Firewood Metrics

Firewood exposes metrics following [Prometheus naming conventions][prom-naming]:
`firewood_{subsystem}_{noun}_{unit}`. Counters always end in `_total`. Histograms
use base-unit suffixes (`_seconds`, `_bytes`). Gauges carry no `_total` suffix.

[prom-naming]: https://prometheus.io/docs/practices/naming/

## Enabling Metrics

### Rust applications

Metrics are automatically recorded as instrumented code paths execute. Wire up
any `metrics`-compatible recorder before starting the database. Example using
the Prometheus exporter:

```rust
use metrics_exporter_prometheus::PrometheusBuilder;

PrometheusBuilder::new()
    .install()
    .expect("failed to install Prometheus recorder");
```

Pass the `HistogramMetricConfig` values returned by each crate's `registry::register()`
to `set_buckets_for_metric` / `set_native_histogram_for_metric` so that the exporter
uses the correct bucket strategy for each histogram.

### Go / FFI applications

```go
import "github.com/ava-labs/firewood/ffi"

if err := ffi.StartMetrics(); err != nil { ... }

gatherer := ffi.Gatherer{}
families, err := gatherer.Gather()
```

`ffi.GatherRenderedMetrics()` (and the `Gatherer` wrapper) merges Rust-side metrics
with Go-side proof-serialization histograms into a single `[]*dto.MetricFamily` slice.

> **Note:** only one metrics instance can be created per process. Calling
> `StartMetrics` a second time returns an error.

## Expensive vs. cheap metrics

Recording overhead is gated by a thread-local `MetricsContext`:

- **Cheap** – always recorded (lock-wait times, flush durations, I/O counters, etc.)
- **Expensive** – recorded only when `MetricsContext::expensive_metrics_enabled()` is
  true for the current thread (e.g. end-to-end proposal duration)

The FFI layer sets the context on each inbound call based on caller configuration.

---

## Metric Reference

### Database layer (`firewood` crate)

#### Proposals

| Metric                                      | Type      | Labels    | Description                                           |
| ------------------------------------------- | --------- | --------- | ----------------------------------------------------- |
| `firewood_proposals_total`                  | counter   | `base`    | Proposals created, by base revision type              |
| `firewood_proposals_discarded_total`        | counter   | —         | Proposals dropped without committing                  |
| `firewood_proposals_uncommitted`            | gauge     | —         | Current count of open, uncommitted proposals          |
| `firewood_proposal_commits_total`           | counter   | `success` | Proposal commit attempts; `success=true\|false`       |
| `firewood_proposal_commit_duration_seconds` | histogram | `success` | End-to-end commit duration; `success=true\|false`     |
| `firewood_proposals_reparented_total`       | counter   | —         | Proposals re-parented to a freshly committed revision |

#### Revisions

| Metric                            | Type    | Labels | Description                                         |
| --------------------------------- | ------- | ------ | --------------------------------------------------- |
| `firewood_revisions_active`       | gauge   | —      | Revisions currently held in memory                  |
| `firewood_revisions_limit`        | gauge   | —      | Configured `max_revisions`                          |
| `firewood_commits_total`          | counter | —      | Revisions durably written to disk                   |
| `firewood_commits_blocked_total`  | counter | —      | Times commit stalled waiting for a persist permit   |
| `firewood_nodes_pending_deletion` | gauge   | —      | Nodes queued for deletion in the committed revision |

#### Proposal creation (expensive)

| Metric                              | Type      | Labels | Description                                                    |
| ----------------------------------- | --------- | ------ | -------------------------------------------------------------- |
| `firewood_propose_duration_seconds` | histogram | —      | End-to-end proposal creation including batch apply and hashing |

#### Merkle trie operations

| Metric                                   | Type    | Labels             | Description                                                                  |
| ---------------------------------------- | ------- | ------------------ | ---------------------------------------------------------------------------- |
| `firewood_node_inserts_total`            | counter | `operation`        | Insert operations by structural result (`update`, `above`, `below`, `split`) |
| `firewood_node_removes_total`            | counter | `prefix`, `result` | Remove operations; `prefix=true\|false`, `result=success\|nonexistent`       |
| `firewood_change_proof_iterations_total` | counter | —                  | Iterator `next()` calls during change-proof generation                       |

#### Persist worker

| Metric                                         | Type      | Labels    | Description                                                                                 |
| ---------------------------------------------- | --------- | --------- | ------------------------------------------------------------------------------------------- |
| `firewood_persist_cycle_duration_seconds`      | histogram | —         | Duration of each background persist-worker cycle (cheap)                                    |
| `firewood_persist_permits_available`           | gauge     | —         | Deferred-persistence permits currently available                                            |
| `firewood_persist_permits_limit`               | gauge     | —         | Maximum deferred-persistence permits configured                                             |
| `firewood_persist_root_store_total`            | counter   | `success` | Root-store persist attempts; `success=true\|false`                                          |
| `firewood_persist_root_store_duration_seconds` | histogram | `success` | Root-store persist duration                                                                 |
| `firewood_persist_submit_duration_seconds`     | histogram | —         | Time to hand a committed revision to the persist-worker channel (cheap, native exponential) |

#### Lock-contention diagnostics (cheap, native exponential histograms)

These measure how long callers wait to _acquire_ the lock, not how long it is held.
High tail latencies indicate write–read contention between concurrent commits and proposals.

| Metric                                        | Type      | Description                                                                                        |
| --------------------------------------------- | --------- | -------------------------------------------------------------------------------------------------- |
| `firewood_commit_lock_wait_seconds`           | histogram | Wait to acquire `in_memory_revisions` write lock in `commit()`                                     |
| `firewood_current_revision_lock_wait_seconds` | histogram | Wait to acquire `in_memory_revisions` read lock in `current_revision()` (called on every proposal) |
| `firewood_by_hash_lock_wait_seconds`          | histogram | Wait to acquire `by_hash` read lock in `revision()`                                                |

---

### Storage layer (`firewood-storage` crate)

#### Space allocation (labeled by `index` = free-list size class)

| Metric                                  | Type    | Labels  | Description                                                       |
| --------------------------------------- | ------- | ------- | ----------------------------------------------------------------- |
| `firewood_storage_bytes_reused_total`   | counter | `index` | Bytes satisfied from a free-list slot                             |
| `firewood_storage_bytes_appended_total` | counter | `index` | Bytes grown at end-of-file (free list had no suitable slot)       |
| `firewood_storage_bytes_freed_total`    | counter | `index` | Bytes returned to the free list                                   |
| `firewood_storage_bytes_wasted_total`   | counter | `index` | Internal fragmentation per allocation: `slot_size − needed_bytes` |
| `firewood_nodes_allocated_total`        | counter | `index` | Node allocations per size class                                   |
| `firewood_nodes_deleted_total`          | counter | `index` | Node deletions per size class                                     |

#### Free lists

| Metric                       | Type  | Labels  | Description                                  |
| ---------------------------- | ----- | ------- | -------------------------------------------- |
| `firewood_free_list_entries` | gauge | `index` | Current entry count per free-list size class |

#### Node reads

| Metric                                   | Type    | Labels         | Description                                |
| ---------------------------------------- | ------- | -------------- | ------------------------------------------ |
| `firewood_node_reads_total`              | counter | `from`         | Node reads by source: `from=cache\|file`   |
| `firewood_node_cache_accesses_total`     | counter | `mode`, `type` | Node cache accesses; `type=hit\|miss`      |
| `firewood_freelist_cache_accesses_total` | counter | `type`         | Free-list cache accesses; `type=hit\|miss` |

#### Node cache size

| Metric                            | Type  | Labels | Description                             |
| --------------------------------- | ----- | ------ | --------------------------------------- |
| `firewood_node_cache_bytes`       | gauge | —      | Memory currently used by the node cache |
| `firewood_node_cache_limit_bytes` | gauge | —      | Configured node-cache memory limit      |
| `firewood_freelist_cache_entries` | gauge | —      | Current free-list cache entry count     |

#### Persistence

| Metric                            | Type      | Labels | Description                                   |
| --------------------------------- | --------- | ------ | --------------------------------------------- |
| `firewood_flush_duration_seconds` | histogram | —      | Node flush duration per persist cycle (cheap) |
| `firewood_reap_duration_seconds`  | histogram | —      | Old-revision reap duration (cheap)            |
| `firewood_database_size_bytes`    | gauge     | —      | Current database file size after each flush   |

#### Disk I/O

| Metric                              | Type      | Labels | Description                                                                                                       |
| ----------------------------------- | --------- | ------ | ----------------------------------------------------------------------------------------------------------------- |
| `firewood_io_reads_total`           | counter   | —      | Number of disk read operations                                                                                    |
| `firewood_io_writes_total`          | counter   | —      | Number of disk write operations                                                                                   |
| `firewood_io_bytes_read_total`      | counter   | —      | Bytes read from disk                                                                                              |
| `firewood_io_bytes_written_total`   | counter   | —      | Bytes written to disk                                                                                             |
| `firewood_io_read_duration_seconds` | histogram | —      | Per-read latency (cheap, native exponential; spans nanoseconds for cached pages to milliseconds for cold storage) |

#### Root store

| Metric                             | Type    | Labels | Description               |
| ---------------------------------- | ------- | ------ | ------------------------- |
| `firewood_rootstore_lookups_total` | counter | —      | Root-store fetch attempts |

#### io_uring (only when `io-uring` feature is enabled)

| Metric                                          | Type    | Labels | Description                                      |
| ----------------------------------------------- | ------- | ------ | ------------------------------------------------ |
| `firewood_io_uring_ring_full_total`             | counter | —      | Times the submission queue was full              |
| `firewood_io_uring_sq_waits_total`              | counter | —      | Submission-queue wait events                     |
| `firewood_io_uring_eagain_retries_total`        | counter | —      | Write entries re-submitted after `EAGAIN`        |
| `firewood_io_uring_partial_write_retries_total` | counter | —      | Write entries re-submitted after a partial write |

---

### FFI / Go layer (`firewood-ffi` crate + `ffi` package)

#### Metrics gathering

| Metric                             | Type      | Labels | Description                                                                      |
| ---------------------------------- | --------- | ------ | -------------------------------------------------------------------------------- |
| `firewood_gather_duration_seconds` | histogram | —      | Wall-clock duration of `GatherRenderedMetrics` calls (cheap, native exponential) |
| `firewood_proof_merges_total`      | counter   | —      | Range-proof merge operations via FFI                                             |

#### Go-side proof serialization

These histograms are recorded in Go and are independent of the Rust recorder.
They measure the full round-trip through the CGo boundary including any byte
copies and pointer conversions.

| Metric                                         | Type      | Labels       | Description                                                           |
| ---------------------------------------------- | --------- | ------------ | --------------------------------------------------------------------- |
| `firewood_go_proof_marshal_duration_seconds`   | histogram | `proof_type` | `MarshalBinary` duration; `proof_type=range\|change\|verified_change` |
| `firewood_go_proof_unmarshal_duration_seconds` | histogram | `proof_type` | `UnmarshalBinary` duration; `proof_type=range\|change`                |

Buckets: `5µs, 25µs, 100µs, 500µs, 1ms, 5ms, 25ms, 100ms`

---

### Replay layer (`firewood-replay` crate)

| Metric                                     | Type      | Labels | Description                            |
| ------------------------------------------ | --------- | ------ | -------------------------------------- |
| `firewood_replay_propose_duration_seconds` | histogram | —      | Propose duration during replay (cheap) |
| `firewood_replay_commit_duration_seconds`  | histogram | —      | Commit duration during replay (cheap)  |

---

### Allocator (`jemalloc`)

Available when Firewood is built with jemalloc (FFI build). The epoch is
advanced before each gather so values reflect the current state.

| Metric                     | Type  | Description                                             |
| -------------------------- | ----- | ------------------------------------------------------- |
| `jemalloc_active_bytes`    | gauge | Bytes in active pages allocated by jemalloc             |
| `jemalloc_allocated_bytes` | gauge | Total bytes allocated by the application                |
| `jemalloc_metadata_bytes`  | gauge | jemalloc internal metadata overhead                     |
| `jemalloc_mapped_bytes`    | gauge | Bytes in active extents mapped by the allocator         |
| `jemalloc_resident_bytes`  | gauge | Bytes in physically resident data pages                 |
| `jemalloc_retained_bytes`  | gauge | Bytes in virtual mappings retained (not returned to OS) |

---

## Example PromQL queries

```promql
# Proposal commit p99 latency (5-minute window)
histogram_quantile(0.99,
  sum(rate(firewood_proposal_commit_duration_seconds_bucket[5m])) by (le)
)

# Commit throughput (commits/sec)
rate(firewood_commits_total[1m])

# Node cache hit rate
sum(rate(firewood_node_cache_accesses_total{type="hit"}[5m]))
  / sum(rate(firewood_node_cache_accesses_total[5m]))

# Commit lock p99 wait (native histogram)
histogram_quantile(0.99,
  sum(rate(firewood_commit_lock_wait_seconds_bucket[5m])) by (le)
)

# Storage fragmentation ratio per size class
rate(firewood_storage_bytes_wasted_total[5m])
  / rate(firewood_storage_bytes_appended_total[5m])

# Database growth rate (bytes/sec)
rate(firewood_storage_bytes_appended_total[5m])

# I/O read throughput (bytes/sec)
rate(firewood_io_bytes_read_total[1m])

# Failed commit ratio
rate(firewood_proposal_commits_total{success="false"}[5m])
  / rate(firewood_proposal_commits_total[5m])
```
