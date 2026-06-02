# Database & Storage Layer

## 1. Purpose

AvalancheGo stores all persistent state (blocks, transactions, validator sets,
chain state, metadata) through a single, composable key-value abstraction.
Rather than coupling each consumer to a concrete engine, the codebase defines a
small `Database` interface ([database/database.go](../database/database.go)) and
builds everything else — backends, namespacing, buffering, metrics, corruption
guards, RPC transport, and the Merkle-trie state store — as implementations or
**decorators** of that interface.

This document describes the storage abstractions only. The business logic of the
VMs that consume them (state schemas, block formats) lives in
[platformvm.md](platformvm.md), [avm.md](avm.md), and [evm.md](evm.md); how each
chain is given its own database is in [chains.md](chains.md).

## 2. Responsibilities & Scope

In scope:

- The core `Database`/`Batch`/`Iterator` interfaces and their behavioral contract.
- Concrete backends: `leveldb`, `pebbledb`, `memdb`.
- Decorators: `prefixdb` (keyspace isolation), `versiondb` (buffered atomic
  commit), `meterdb` (metrics), `corruptabledb` (corruption fail-stop),
  `rpcdb` (gRPC transport for out-of-process VMs).
- Structured helpers built on `Database`: `linkeddb` (ordered list),
  `heightindexdb`, `x/blockdb` (raw block files), `x/archivedb` (height-versioned).
- `x/merkledb` — the verifiable Merkle-radix-trie store used for state sync.
- `cache/` — the LRU + metered caching layer used throughout.

Out of scope (cross-referenced): VM state machines, consensus, networking
([networking.md](networking.md)), the state-sync protocol drivers above merkledb.

## 3. Package / File Layout

| Path | Role |
|------|------|
| [database/database.go](../database/database.go) | Core interfaces (`Database`, KV reader/writer/deleter, `Compacter`, `HeightIndex`) |
| [database/batch.go](../database/batch.go) | `Batch`/`Batcher` interfaces + `BatchOps` helper |
| [database/iterator.go](../database/iterator.go) | `Iterator`/`Iteratee` + `IteratorError` |
| [database/errors.go](../database/errors.go) | `ErrClosed`, `ErrNotFound` |
| [database/common.go](../database/common.go) | Batch capacity-shrink constants |
| [database/helpers.go](../database/helpers.go) | Typed get/put helpers (uint64, IDs, etc.) |
| [database/factory/factory.go](../database/factory/factory.go) | Backend selection + wrapping |
| [database/leveldb/db.go](../database/leveldb/db.go) | LevelDB (goleveldb) backend |
| [database/pebbledb/db.go](../database/pebbledb/db.go) | PebbleDB (CockroachDB pebble) backend |
| [database/memdb/db.go](../database/memdb/db.go) | In-memory map backend |
| [database/prefixdb/db.go](../database/prefixdb/db.go) | Keyspace namespacing decorator |
| [database/versiondb/db.go](../database/versiondb/db.go) | In-memory write buffer with atomic commit |
| [database/meterdb/db.go](../database/meterdb/db.go) | Prometheus metrics decorator |
| [database/corruptabledb/db.go](../database/corruptabledb/db.go) | Fail-stop on unexpected errors |
| [database/rpcdb/db_client.go](../database/rpcdb/db_client.go), [db_server.go](../database/rpcdb/db_server.go) | gRPC client/server bridge |
| [database/linkeddb/linkeddb.go](../database/linkeddb/linkeddb.go) | Doubly-linked-list KV store |
| [database/heightindexdb/](../database/heightindexdb/) | `HeightIndex` implementations (memdb, meterdb) |
| [x/merkledb/](../x/merkledb/) | Merkle-radix-trie database |
| [x/blockdb/](../x/blockdb/) | Append-only raw block file store |
| [x/archivedb/](../x/archivedb/) | Height-versioned append-only KV |
| [cache/cache.go](../cache/cache.go) | `Cacher` interface |
| [cache/lru/](../cache/lru/) | LRU + sized LRU caches |
| [cache/metercacher/](../cache/metercacher/) | Metrics wrapper for caches |

## 4. Core Interfaces

### 4.1 `Database`

`Database` is an aggregation of small single-method interfaces
([database/database.go:89](../database/database.go)):

```go
type Database interface {
    KeyValueReaderWriterDeleter   // Has / Get / Put / Delete
    Batcher                       // NewBatch
    Iteratee                      // NewIterator*
    Compacter                     // Compact
    io.Closer                     // Close
    health.Checker                // HealthCheck
}
```

The constituent KV methods ([database.go:17-69](../database/database.go)):

```go
Has(key []byte) (bool, error)
Get(key []byte) ([]byte, error)   // returns ErrNotFound if absent
Put(key, value []byte) error      // nil key == empty key; nil value may read back nil-or-empty
Delete(key []byte) error
Compact(start, limit []byte) error // nil start = -inf, nil limit = +inf
```

Documented argument-aliasing contract: `key`/`value` are always **safe to modify
after the call returns**; returned byte slices from `Get` are **safe to read but
not to modify**. Implementations clone as needed.

The interface deliberately mirrors go-ethereum's `ethdb` so that geth-derived EVM
code can use it unchanged ([database.go:4-6](../database/database.go)).

### 4.2 `Batch` / `Batcher`

A `Batch` is a write-only buffer flushed atomically by `Write`
([database/batch.go:14](../database/batch.go)):

```go
type Batch interface {
    KeyValueWriterDeleter
    Size() int                              // buffered bytes (keys+values+deletes)
    Write() error                           // atomic flush to host db
    Reset()                                 // clear for reuse
    Replay(w KeyValueWriterDeleter) error   // re-apply ops, in order, to another target
    Inner() Batch                           // innermost batch (used when decorators nest)
}
```

`BatchOps` ([batch.go:50](../database/batch.go)) is a reusable helper that records
cloned ops and implements `Replay`; versiondb and others embed it.
`common.go` defines `MaxExcessCapacityFactor`/`CapacityReductionFactor`
([common.go:13](../database/common.go)) controlling when a batch's backing slice
is shrunk on `Reset` to bound retained memory.

### 4.3 `Iterator` / `Iteratee`

`Iterator` ([database/iterator.go:21](../database/iterator.go)) is a lazy cursor.
Contract:

- `Next()` returns `false` on exhaustion **or** error; `Error()` is checked after.
- A closed DB makes `Next()` return `false` and `Error()` return `ErrClosed`.
- Not safe for concurrent use; multiple iterators may run concurrently.
- `Release()` is mandatory, idempotent, and may be called before exhaustion.

`Iteratee` provides four constructors ([iterator.go:51](../database/iterator.go)):
full, `WithStart`, `WithPrefix`, `WithStartAndPrefix`. `IteratorError`
([iterator.go:71](../database/iterator.go)) is a no-op iterator that only reports a
fixed error — returned by decorators when the DB is already closed.

### 4.4 Errors

Only two sentinel errors ([database/errors.go](../database/errors.go)):
`ErrClosed` and `ErrNotFound`. These are treated as **expected** (non-corrupting)
everywhere; any other error is treated as potential corruption (see §7.3).

### 4.5 `HeightIndex`

A specialized height-keyed store ([database.go:99](../database/database.go)):
`Put(height, value)`, `Get(height)`, `Has(height)`, `Sync(start,end)`, `Close()`.
Implemented by [x/blockdb](../x/blockdb/database.go) and wrapped by
[database/heightindexdb/meterdb](../database/heightindexdb/meterdb/db.go).

## 5. The Decorator Stack

Every decorator implements `database.Database` by wrapping another
`database.Database`, so they compose freely. The runtime stack for a typical
chain (assembled in [chains/manager.go:635-645](../chains/manager.go) and
[database/factory/factory.go:59-62](../database/factory/factory.go)):

```
                 per-chain / per-component prefixdb (keyspace isolation)
                          prefixdb(VMDBPrefix), prefixdb(VertexDBPrefix), ...
                                        |
                          prefixdb(ChainID)            <- chains/manager.go:640
                                        |
                          meterdb (per-chain metrics)  <- chains/manager.go:635
                                        |
        ----  shared base DB (built once at node startup)  ----
                          versiondb (only if readOnly)  <- factory.go:61
                                        |
                          corruptabledb (fail-stop)     <- factory.go:59
                                        |
                  leveldb | pebbledb | memdb (the concrete engine)
```

`versiondb` is layered on top per write transaction by VMs (e.g. for atomic block
commit, §6.2) rather than only at the base; `factory.New` adds one only for
read-only mode.

### 5.1 `prefixdb` — keyspace isolation

`prefixdb.New(prefix, db)` ([prefixdb/db.go:62](../database/prefixdb/db.go))
prepends every key with a fixed prefix and confines iteration/compaction to
`[prefix, increment(prefix))` ([db.go:48](../database/prefixdb/db.go) computes the
limit; [db.go:175](../database/prefixdb/db.go) scopes iterators).

- The prefix stored is `hashing.ComputeHash256(prefix)`
  ([db.go:84](../database/prefixdb/db.go)), giving every prefix a fixed 32-byte
  length so prefixes can't ambiguously overlap.
- Nesting is flattened: wrapping a `prefixdb` in another `prefixdb` joins+rehashes
  the prefixes and points at the **original base** db, avoiding deep call chains
  ([db.go:63-68](../database/prefixdb/db.go)). `NewNested`
  ([db.go:77](../database/prefixdb/db.go)) opts out of this compression.
- Iterators strip the prefix from returned keys
  ([db.go:358](../database/prefixdb/db.go)).
- Closing a prefixdb only marks it closed; it does **not** close the shared base.
- A pooled `BytesPool` ([db.go:247](../database/prefixdb/db.go)) reuses prefixed-key
  buffers to limit allocations on the hot path.

This is how each chain gets an isolated namespace: `prefixdb.New(ChainID, ...)`
then further `prefixdb.New(VMDBPrefix, ...)` etc. (see [chains.md](chains.md)).

### 5.2 `versiondb` — buffered atomic commit

`versiondb.New(db)` ([versiondb/db.go:46](../database/versiondb/db.go)) holds all
writes in an in-memory `map[string]valueDelete`
([db.go:35-43](../database/versiondb/db.go)) and only touches the underlying DB on
`Commit`:

- `Put`/`Delete` mutate the map; `Get`/`Has` read the map first, then fall through
  to the base DB ([db.go:54-103](../database/versiondb/db.go)).
- `Commit` ([db.go:186](../database/versiondb/db.go)) replays the buffered map into
  a single base-DB batch and calls `Write()` — one atomic flush — then clears the
  buffer. `Abort` ([db.go:203](../database/versiondb/db.go)) just clears the map.
- `CommitBatch` ([db.go:218](../database/versiondb/db.go)) returns the prepared
  batch without writing, letting a caller fold these writes into a larger atomic
  operation (used so a VM can commit DB state and shared-memory atomically).
- It implements `Commitable` ([db.go:25](../database/versiondb/db.go)).
- Its iterator merges the sorted in-memory keys with the base iterator
  ([db.go:321](../database/versiondb/db.go)), honoring buffered deletes.
- `SetDatabase`/`GetDatabase` ([db.go:164](../database/versiondb/db.go)) allow
  swapping the base (used during bootstrap/state-sync swaps).

### 5.3 `meterdb` — metrics

`meterdb.New(reg, db)` ([meterdb/db.go](../database/meterdb/db.go)) times every
method and counts bytes read/written, exporting per-method Prometheus metrics
(one label per method — `has`, `get`, `put`, `batch_write`, `iterator_next`, …
[db.go:21-85](../database/meterdb/db.go)). It is placed per-chain just under the
chain's prefixdb so each chain reports independently.

### 5.4 `corruptabledb` — fail-stop

`corruptabledb.New(db, log)` ([corruptabledb/db.go:36](../database/corruptabledb/db.go))
guards against silent data corruption. `handleError`
([db.go:134](../database/corruptabledb/db.go)) inspects every returned error: if it
is anything other than `nil`, `ErrNotFound`, or `ErrClosed`, the wrapper latches
that error and **every subsequent operation fails** with it
([db.go:127-155](../database/corruptabledb/db.go)). This prevents the node from
continuing to operate on a possibly-corrupted disk. It sits directly above the
backend in `factory.New`.

### 5.5 `rpcdb` — serving the DB over gRPC

`rpcdb` bridges the `Database` interface across a process boundary for
out-of-process (plugin) VMs (see [vm-framework.md](vm-framework.md),
`vms/rpcchainvm`):

- **Server** `rpcdb.NewServer(db)` ([db_server.go:41](../database/rpcdb/db_server.go))
  wraps a real `Database` and answers `rpcdbpb` RPCs (`Has`, `Get`, `Put`,
  `WriteBatch`, iterator lifecycle). It runs in the avalanchego process; the VM
  plugin connects as a client ([vms/rpcchainvm/vm_client.go:293](../vms/rpcchainvm/vm_client.go)).
- **Client** `rpcdb.NewClient(...)` ([db_client.go:34](../database/rpcdb/db_client.go))
  implements `Database` by issuing those RPCs. Used inside the plugin process
  ([vms/rpcchainvm/vm_server.go:196](../vms/rpcchainvm/vm_server.go)).
- Sentinel errors are encoded as enum values across the wire and decoded back
  ([rpcdb/errors.go](../database/rpcdb/errors.go)): only `ErrClosed`/`ErrNotFound`
  round-trip as "expected"; anything else is a real gRPC error.
- Iterators are streamed in **batches**: a background `fetch` goroutine pulls pages
  of key/values, and the invariant that exactly one outstanding request exists per
  server-side iterator id is enforced client-side
  ([db_client.go:215-219](../database/rpcdb/db_client.go)). Batching amortizes the
  per-key RPC cost.

### 5.6 `linkeddb` — ordered list on top of a KV store

`linkeddb` ([linkeddb/linkeddb.go:27](../database/linkeddb/linkeddb.go)) implements
a doubly-linked list over an arbitrary `Database`, giving insertion-ordered
iteration that a plain KV store (sorted by key) can't. Each entry is a `node`
with `Next`/`Previous` pointers ([linkeddb.go:56](../database/linkeddb/linkeddb.go));
a head pointer at key `0x01` anchors the list. It caches nodes in an LRU
([linkeddb.go:64](../database/linkeddb/linkeddb.go)) and batches writes. Used by
mempools and similar ordered collections.

## 6. Concrete Backends & the Factory

### 6.1 Backends

| Backend | Engine | Notes |
|---------|--------|-------|
| `leveldb` | goleveldb (pure Go) | Defaults: 12 MiB block cache, 12 MiB write buffer, 1024 file handles, bloom 10 bits/key ([leveldb/db.go:30-62](../database/leveldb/db.go)). Reports disk stats via `HealthCheck`. |
| `pebbledb` | cockroachdb/pebble | Defaults: 512 MiB cache, memtable = cache/4, stop-writes threshold 8 ([pebbledb/db.go:23-44](../database/pebbledb/db.go)). |
| `memdb` | in-process `map[string][]byte` | Ephemeral, RW-locked ([memdb/db.go:30](../database/memdb/db.go)); used for tests and read-only/no-persistence modes. |

Both disk engines take a JSON config blob letting operators override the defaults.

### 6.2 Factory & wrapping

`factory.New(name, path, readOnly, config, reg, logger)`
([factory/factory.go:28](../database/factory/factory.go)) selects the engine by
name (`leveldb` | `memdb` | `pebbledb`), then **always** wraps it in
`corruptabledb`, and additionally wraps in `versiondb` when `readOnly` and not
memdb ([factory.go:59-62](../database/factory/factory.go)). The node builds this
base DB once and hands it to the chain manager, which adds the per-chain
`meterdb` + `prefixdb` layers (§5).

**Atomic block commit pattern:** a VM accumulates a block's state changes in a
`versiondb` over its chain DB; once the block is accepted, `Commit` flushes them
in one base-DB batch, so the engine sees the whole block atomically (and a crash
leaves either the whole block or none of it). When the block must also write
cross-chain shared memory, `CommitBatch` is used to merge both into one write
([chains/atomic/shared_memory.go:130](../chains/atomic/shared_memory.go)).

## 7. Component Boundaries & Relationships

### 7.1 DB ↔ VM / chain state

The chain manager owns the only references to the base engine. Each chain receives
a `prefixdb(ChainID)` sub-namespace and, within it, further prefixes for the VM,
vertex, and bootstrap stores ([chains/manager.go:640-645](../chains/manager.go)).
A VM never sees the engine, only its `database.Database` handle — so it cannot
read or corrupt another chain's keyspace. See [chains.md](chains.md),
[vm-framework.md](vm-framework.md).

### 7.2 DB ↔ rpcchainvm plugin (rpcdb)

For plugin VMs the boundary is a gRPC channel: avalanchego runs
`rpcdb.NewServer` over the chain's real DB, and the plugin process holds an
`rpcdb` client implementing `database.Database`. The plugin therefore programs
against the identical interface whether in- or out-of-process. The corruption and
metrics decorators stay on the server side. See [vm-framework.md](vm-framework.md).

### 7.3 Corruption isolation

`ErrNotFound`/`ErrClosed` are control-flow signals, not failures. Any other error
bubbling out of an engine is assumed to indicate disk/data corruption:
`corruptabledb` latches it and refuses further work, and `rpcdb` propagates it as a
non-sentinel gRPC error. This keeps a damaged node from compounding corruption.

### 7.4 merkledb ↔ state sync

`x/merkledb`'s `MerkleDB` satisfies the generic
`merklesync.DB[*RangeProof, *ChangeProof]` interface
([x/merkledb/db.go:42](../x/merkledb/db.go), [database/merkle/sync/db.go:25](../database/merkle/sync/db.go)),
which the state-sync engine uses to fetch/verify/commit ranges from peers. The
sync driver itself ([database/merkle/sync/](../database/merkle/sync/)) is out of
scope here; merkledb only provides the proofs.

## 8. merkledb — the verifiable trie store

`x/merkledb` is a key-value store backed by a **Merkle radix trie**: it behaves
like a `database.Database` but additionally exposes a cryptographic root hash and
proofs, enabling trustless state sync ([x/merkledb/README.md](../x/merkledb/README.md)).

### 8.1 Trie structure & hashing

- A trie is made of `node`s; each node's key is a prefix of its children's keys
  and it has up to `BranchFactor` children. Supported branch factors are 2/16/256,
  mapping to token sizes 1/4/8 bits ([x/merkledb/key.go:20](../x/merkledb/key.go));
  the default is `BranchFactor16` ([db.go:199](../x/merkledb/db.go)).
- Each node has an **ID** = hash of its contents. The `Hasher` interface hashes a
  node from its children IDs, value (or value-hash if long), and key
  ([x/merkledb/hashing.go:25](../x/merkledb/hashing.go), `HashLength = 32`,
  default SHA-256). Changing any node changes its ID and cascades to the **root
  ID**, which uniquely identifies a *revision* of the state.
- Persistence splits nodes into **value nodes** (prefix `1`) and **intermediate
  nodes** (prefix `2`), plus metadata (prefix `0`), each in its own backing store
  ([db.go:44-46, 263-264](../x/merkledb/db.go)). A `cleanShutdownKey`
  ([db.go:48-64](../x/merkledb/db.go)) records whether intermediate nodes were
  flushed cleanly; on an unclean shutdown they are rebuilt from value nodes
  ([db.go:397 `rebuild`](../x/merkledb/db.go)).

### 8.2 Views

A **view** ([x/merkledb/view.go](../x/merkledb/view.go)) is an immutable proposed
set of changes layered on the DB or another view. Changes and node IDs are
computed **lazily** (only when a root hash or commit is needed) to avoid hashing
on every edit. Views chain into a tree; a view may be committed only if its parent
is the DB and only once. Committing a view **invalidates** its siblings and their
descendants (they then return `ErrInvalid`). Writes to the DB itself go through an
internal view (`NewBatch`/`Put`/`Delete` build one).

### 8.3 Proofs

Defined in [x/merkledb/proof.go](../x/merkledb/proof.go):

- **`Proof`** ([proof.go:112](../x/merkledb/proof.go)): a single-key
  inclusion/exclusion proof — the node path from root to the (would-be) target.
  A verifier reconstructs a trie from the path and checks its root equals the
  trusted root ID.
- **`RangeProof`** ([proof.go:184](../x/merkledb/proof.go)): proves a contiguous,
  complete set of key/value pairs in `[start, end]` at a root (start proof + end
  proof + the pairs). Lets a syncing client download many pairs at once and verify
  there are no omitted keys in the range.
- **`ChangeProof`** ([proof.go:349](../x/merkledb/proof.go)) + `KeyChange`
  ([proof.go:341](../x/merkledb/proof.go)): proves the set of key changes between
  two root IDs over a range — used to *catch up* a partially-synced DB without
  re-downloading unchanged state.

The DB serves these via `GetProof`, `GetRangeProofAtRoot`, `GetChangeProof` and
verifies/commits peer proofs via `VerifyRangeProof`/`CommitRangeProof`,
`VerifyChangeProof`/`CommitChangeProof` ([db.go:70-165, 713-875](../x/merkledb/db.go)).
Change proofs require sufficient retained history (`HistoryLength`, default 300
revisions, [db.go:202](../x/merkledb/db.go)); the `trieHistory`
([x/merkledb/history.go:21](../x/merkledb/history.go)) records recent change
summaries in memory.

### 8.4 Proof flow (state sync)

```
syncing client                         serving peer (MerkleDB)
   | known trusted endRootID r              |
   |--- GetRangeProof(start,end,r) -------->|  GetRangeProofAtRoot
   |<-- RangeProof{startPf,endPf,kvs} ------|
   | VerifyRangeProof(r) -> ok              |
   | CommitRangeProof  (writes kvs locally) |
   | ... repeat for next range ...          |
   | later, to catch up old->new root:      |
   |--- GetChangeProof(oldR,newR,range) --->|
   |<-- ChangeProof{keyChanges,...} --------|
   | VerifyChangeProof / CommitChangeProof  |
```

## 9. x/blockdb and x/archivedb

- **`x/blockdb`** ([x/blockdb/database.go](../x/blockdb/database.go)) is a
  purpose-built store for raw block bytes, implementing `database.HeightIndex`
  ([database.go:53](../x/blockdb/database.go)). Blocks are appended to flat data
  files (`blockdb_N.dat`) with a separate index file (`blockdb.idx`); each entry
  carries a height, size, xxhash checksum, and version
  ([database.go:30-44, blockEntryHeader](../x/blockdb/database.go)) and may be
  zstd-compressed. It supports recovery of the index from the data files and an
  LRU read cache ([x/blockdb/cache_db.go](../x/blockdb/cache_db.go)). This avoids
  LSM write-amplification for large, write-once block payloads.
- **`x/archivedb`** ([x/archivedb/db.go](../x/archivedb/db.go)) is an append-only,
  height-versioned KV store: each `NewBatch(height)` records puts/deletes tagged
  with that height, and reads ask for a key *as of* a height, returning the most
  recent value at or below it ([db.go:23-44](../x/archivedb/db.go)). It keeps the
  full history of every key (used for historical/archival state queries) rather
  than overwriting in place.

## 10. Caching Layer (`cache/`)

`Cacher[K,V]` ([cache/cache.go:7](../cache/cache.go)) is a best-effort,
evicting key-value cache: `Put`, `Get`, `Evict`, `Flush`, `Len`, `PortionFilled`.

- **`lru.Cache`** ([cache/lru/cache.go:29](../cache/lru/cache.go)): count-bounded
  LRU; optional `OnEvict` callback. **`lru.SizedCache`**
  ([cache/lru/sized_cache.go:37](../cache/lru/sized_cache.go)): byte-size-bounded
  via a per-entry size function (used where values vary in size). `Deduplicator`
  ([cache/lru/deduplicator.go](../cache/lru/deduplicator.go)) collapses in-flight
  duplicate work.
- **`metercacher.Cache`** ([cache/metercacher/cache.go:16](../cache/metercacher/cache.go)):
  decorates any `Cacher` with hit/miss/eviction Prometheus metrics.
- **`cache.Empty`** ([cache/empty.go](../cache/empty.go)): a no-op cache for
  disabling caching without branching.

Consumers include merkledb's value/intermediate node caches
([db.go:203-204](../x/merkledb/db.go)), `linkeddb`'s node cache, blockdb's read
cache, and many VM state caches.

## 11. Key Behaviors & Invariants

- **Atomicity.** `Batch.Write` and `versiondb.Commit` flush all buffered ops as one
  unit; partial application is not observable across a crash for the disk engines.
- **Isolation.** `prefixdb` (with hashed, fixed-length prefixes) guarantees a chain
  cannot read or iterate outside its namespace; closing a sub-DB never closes the
  shared base.
- **Argument aliasing.** Callers may reuse their key/value buffers immediately after
  any call; implementations clone. Returned slices from `Get` must not be mutated.
- **Closed semantics.** Operations after `Close` return `ErrClosed`; iterators on a
  closed DB stop and report `ErrClosed`.
- **Fail-stop on corruption.** Unexpected (non-sentinel) errors latch
  `corruptabledb` and halt further DB use.
- **merkledb determinism.** Identical key/value sets always yield identical root IDs
  regardless of insertion order; view changes are hashed lazily and concurrently
  (`RootGenConcurrency`).
- **Performance.** Hot paths pool byte buffers (prefixdb), shrink oversized batch
  slices (`common.go`), batch rpcdb iterator pages, and cache trie nodes; choose
  `pebbledb` for large state, `leveldb` as the legacy default, `memdb` for tests.

## 12. Configuration

| Setting | Where | Default |
|---------|-------|---------|
| DB engine (`db-type`) | `factory.New` name arg | `leveldb` \| `pebbledb` \| `memdb` |
| Read-only mode | `factory.New` `readOnly` | wraps in `versiondb` (non-memdb) |
| Engine JSON config | `factory.New` `config` | per-engine overrides of cache/buffer/handles |
| LevelDB block cache / write buffer | [leveldb/db.go:36,40](../database/leveldb/db.go) | 12 MiB / 12 MiB |
| LevelDB file handle cap / bloom bits | [leveldb/db.go:44,48](../database/leveldb/db.go) | 1024 / 10 |
| Pebble cache / memtable / stop-writes | [pebbledb/db.go:30,42,43](../database/pebbledb/db.go) | 512 MiB / cache÷4 / 8 |
| merkledb branch factor | [db.go:199](../x/merkledb/db.go) | `BranchFactor16` |
| merkledb history length | [db.go:202](../x/merkledb/db.go) | 300 revisions |
| merkledb value/intermediate node cache | [db.go:203-204](../x/merkledb/db.go) | 1 MiB each |
| linkeddb node cache | [linkeddb.go:16](../database/linkeddb/linkeddb.go) | 1024 entries |

Node-level wiring of these (CLI flags, where the base DB is created) is in
[node.md](node.md).

## 13. Cross-References

- [overview.md](overview.md) — system overview.
- [chains.md](chains.md) — per-chain prefixdb namespacing and DB handoff.
- [vm-framework.md](vm-framework.md) — VM↔DB boundary and rpcchainvm/rpcdb plugins.
- [platformvm.md](platformvm.md), [avm.md](avm.md), [evm.md](evm.md) — DB consumers.
- [node.md](node.md) — base DB construction and config flags.
- [networking.md](networking.md) — transport for state-sync proofs.
- [primitives.md](primitives.md) — `ids`, `maybe`, hashing utilities used here.
- [consensus.md](consensus.md), [simplex.md](simplex.md), [api.md](api.md) — siblings.
