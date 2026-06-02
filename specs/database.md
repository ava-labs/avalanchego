# Database Layer

## 1. Core Interface (`database/`)

All storage in AvalancheGo goes through a composable interface hierarchy:

```go
// database/database.go
type Database interface {
    KeyValueReaderWriterDeleter
    Batcher
    Iteratee
    Compacter
    io.Closer
    health.Checker
}

type KeyValueReader interface {
    Has(key []byte) (bool, error)
    Get(key []byte) ([]byte, error)  // ErrNotFound if missing
}

type KeyValueWriter interface {
    Put(key, value []byte) error
}

type KeyValueDeleter interface {
    Delete(key []byte) error
}

type Compacter interface {
    Compact(start, limit []byte) error  // nil = infinity
}
```

### 1.1 Batch Interface

```go
type Batch interface {
    KeyValueWriterDeleter
    Size() int              // total buffered bytes
    Write() error           // atomic flush
    Reset()                 // clear for reuse
    Replay(w KeyValueWriterDeleter) error  // apply to another target
    Inner() Batch           // innermost batch in chain
}
```

`Write()` is all-or-nothing: either all ops succeed or none do.

### 1.2 Iterator Interface

```go
type Iterator interface {
    Next() bool
    Error() error
    Key() []byte     // valid until Release()
    Value() []byte   // valid until Release()
    Release()        // must be called; safe to call multiple times
}

type Iteratee interface {
    NewIterator() Iterator
    NewIteratorWithStart(start []byte) Iterator
    NewIteratorWithPrefix(prefix []byte) Iterator
    NewIteratorWithStartAndPrefix(start, prefix []byte) Iterator
}
```

### 1.3 Error Semantics

| Error | Meaning |
|-------|---------|
| `ErrNotFound` | Key does not exist (Get, Has) |
| `ErrClosed` | Database was closed |
| All others | I/O or corruption errors |

---

## 2. Storage Implementations

### 2.1 MemDB (`database/memdb/`)

In-memory, ephemeral key-value store.

```go
type Database struct {
    lock sync.RWMutex
    db   map[string][]byte
}
```

- Keys stored as strings; values cloned on read/write.
- Iterator snapshots keys at creation time (safe from concurrent writes).
- `Compact()` is a no-op.
- Use: testing, caching layers, ephemeral state.

### 2.2 LevelDB (`database/leveldb/`)

Wraps `syndtr/goleveldb`. Default database engine.

**Config defaults:**
```
BlockCacheCapacity:   12 MiB      (64-bit blocks cached in memory)
WriteBuffer:          6 MiB each  (two memtables before flush)
OpenFilesCacheCapacity: 1024      (file descriptors)
CompactionTotalSize:  10 MiB base, 10× multiplier per level
BloomFilter:          10 bits/key
```

- Background goroutine polls LevelDB stats every 10s for Prometheus metrics.
- Batch overhead: 8 bytes per operation (for size tracking).
- `Compact(nil, nil)` triggers full compaction.

**Batch:**
- Wraps `leveldb.Batch`.
- `Replay()` reconstructs ops by re-running Put/Delete on target.

### 2.3 PebbleDB (`database/pebbledb/`)

CockroachDB's embedded database; alternative to LevelDB.

**Config defaults:**
```
CacheSize:          512 MiB
BytesPerSync:       512 KiB
MaxOpenFiles:       4096
ReadSamplingMultiplier: -1 (disabled; prevents compaction write stalls)
MaxConcurrentCompactions: 1
```

- Iterators registered in `openIterators` set; auto-closed on DB close.
- Batch idempotence: second `Write()` call clones the batch first (pebble limitation).
- Upper bound calculation for prefix iteration: increment last non-0xFF byte.

### 2.4 VersionDB (`database/versiondb/`)

In-memory copy-on-write layer over any Database. Enables state diffs and rollbacks.

```go
type Database struct {
    lock  sync.RWMutex
    mem   map[string]valueDelete   // pending changes
    db    database.Database        // underlying DB
    batch database.Batch
}

type valueDelete struct {
    value  []byte
    delete bool  // true = deletion in the map
}
```

**Operations:**
- `Commit()`: Flush `mem` → `db.batch` → `batch.Write()` → clear `mem`.
- `Abort()`: Clear `mem` without writing.
- `CommitBatch()`: Return batch without flushing (caller controls write timing).
- `SetDatabase(newDB)`: Hot-swap underlying DB (used by state sync).

**Iterator:** Merges in-memory map with underlying DB iterator. In-memory writes take precedence over disk reads for the same key.

**Use:** P-Chain and X-Chain state diffs; any place where "apply if accepted, discard if rejected" semantics are needed.

### 2.5 PrefixDB (`database/prefixdb/`)

Namespace isolation by prepending a fixed-length prefix to all keys.

```go
type Database struct {
    dbPrefix   []byte   // Hash256(userPrefix) — fixed 32 bytes
    dbLimit    []byte   // dbPrefix + 1 (upper bound)
    db         database.Database
}
```

- Prefix is a SHA256 hash of the user-supplied prefix bytes; gives fixed 32-byte overhead regardless of prefix length.
- `JoinPrefixes(p1, p2)` = `Hash256(p1 || p2)` for nested namespaces.
- Iterator strips prefix from returned keys.
- Batches pool-manage `[]byte` buffers for efficiency.

**Use:** Isolating different state namespaces (UTXOs, validators, transactions) within a single physical DB file.

### 2.6 LinkedDB (`database/linkeddb/`)

Ordered doubly-linked list on top of a key-value store.

```go
type node struct {
    Value       []byte
    HasNext     bool
    Next        []byte
    HasPrevious bool
    Previous    []byte
}
```

- `Put(key, value)`: If key exists, update; else insert as new head.
- `Delete(key)`: Remove and relink neighbors.
- `HeadKey()`: Cached.
- Iterator walks `Next` pointers from head.

**Caching:** LRU node cache (default 1024 entries). Head key separately cached with dirty bit.

**Use:** Transaction ordering, FIFO queues, ordered event logs.

### 2.7 RPCDB (`database/rpcdb/`)

Database over gRPC for inter-process communication.

**Server** (`DatabaseServer`):
```go
type DatabaseServer struct {
    db database.Database
    iteratorLock sync.RWMutex
    iterators map[uint64]database.Iterator
}
```

Allocates iterators by ID. `IteratorNext` returns up to 128 KiB per RPC call (batched).

**Client** (`DatabaseClient`):
- Implements `database.Database` by calling gRPC stubs.
- Batch deduplication: strips duplicate keys (keeps last write).
- Streaming iterator: background goroutine fetches batches; client consumes locally.

**Error mapping:** gRPC status codes ↔ `database.ErrNotFound` / `database.ErrClosed`.

**Use:** VM subprocess (RPCChainVM) database access; node isolation.

### 2.8 CorruptableDB (`database/corruptabledb/`)

Wrapper that detects unexpected errors and prevents further access.

```go
type Database struct {
    database.Database
    errorLock    sync.RWMutex
    initialError error  // first non-recoverable error
}
```

- `ErrNotFound`, `ErrClosed` are allowed (no action).
- Any other error: sets `initialError` once; all subsequent ops return that error.
- Prevents cascade corruption from partial writes.

### 2.9 MeterDB (`database/meterdb/`)

Prometheus metrics decorator for any Database.

Metrics collected (labeled by method):
- `calls` counter
- `duration` gauge (cumulative nanoseconds)
- `size` counter (bytes read/written)

Instruments: all `Database`, `Batch`, and `Iterator` methods.

### 2.10 HeightIndexDB (`database/heightindexdb/`)

Maps `uint64` height → `[]byte` value.

```go
type HeightIndex interface {
    Put(height uint64, value []byte) error
    Get(height uint64) ([]byte, error)
    Has(height uint64) (bool, error)
    Sync(start, end uint64) error
}
```

In-memory implementation uses `map[uint64][]byte`.
Use: block height → block ID index.

---

## 3. Implementation Comparison

| Feature | MemDB | LevelDB | PebbleDB | VersionDB | PrefixDB | LinkedDB | RPCDB |
|---------|-------|---------|----------|-----------|----------|----------|-------|
| Persistence | None | Disk | Disk | Via parent | Via parent | Via parent | Remote |
| Thread safety | RWMutex | Internal | Pebble locks | RWMutex | RWMutex | RWMutex | Network |
| Key ordering | Lexicographic | Lexicographic | Lexicographic | Lexicographic | Lexicographic | Insertion order | Parent |
| ACID batches | Yes | Yes | Yes | Yes | Yes | Yes | Yes |
| Compaction | No-op | Yes | Yes | Via parent | Via parent | N/A | Delegated |
| Primary use | Testing | Default storage | High-throughput | State diffs | Namespacing | Ordered lists | IPC |

---

## 4. MerkleDB — PATRICIA Trie (`x/merkledb/`)

### 4.1 Architecture

```go
type merkleDB struct {
    baseDB             database.Database
    valueNodeDB        *valueNodeDB         // leaf nodes
    intermediateNodeDB *intermediateNodeDB  // internal nodes
    history            *trieHistory         // for change proofs
    root               maybe.Maybe[*node]
    rootID             ids.ID
}
```

A radix/PATRICIA trie with configurable branch factor.

### 4.2 Branch Factors

| BranchFactor | Bits per token | Max trie depth for 256-bit key |
|-------------|---------------|-------------------------------|
| 2 | 1 bit | 256 |
| 4 | 2 bits | 128 |
| 16 | 4 bits | 64 |
| 256 | 8 bits | 32 |

Keys are divided into tokens of N bits. Each token is an index into the node's `Children` array.

### 4.3 Node Structure

```go
type node struct {
    Key      Key             // full path from root to this node
    Children [BranchFactor]*node
    Value    []byte          // nil for internal nodes; non-nil for leaves
    hash     ids.ID          // cached node hash
}

type Key struct {
    length int     // bit length
    value  string  // compact bit string
}
```

### 4.4 Storage Layout

Three namespaces in `baseDB` (via PrefixDB):

| Prefix | Content |
|--------|---------|
| `0x00` | Metadata: `cleanShutdown`, `rootKey` |
| `0x01` | Value nodes: `key → value bytes` |
| `0x02` | Intermediate nodes: `key → serialized children` |

### 4.5 View / Diff Model

```go
type View interface {
    Get(key []byte) ([]byte, error)
    Put(key, value []byte) error
    Delete(key []byte) error
    NewView(ctx, ViewChanges) (View, error)
    CommitToDB(ctx) error
    GetMerkleRoot(ctx) (ids.ID, error)
    GetProof(ctx, key []byte) (*Proof, error)
}
```

- Lazy clone-on-write: shares unchanged subtrees with parent view.
- In-memory change buffer until `CommitToDB()`.
- Child views track parent invalidation; concurrent views allowed.
- `CommitToDB()` flushes all changes to `merkleDB` atomically.

### 4.6 Caching

**ValueNodeDB caching:**
```go
type valueNodeDB struct {
    baseDB    database.Database
    nodeCache cache.Cacher[Key, *node]
}
```
Cache miss: deserialize from `baseDB` prefix `0x01`.

**IntermediateNodeDB — three-tier:**
1. `writeBuffer` (dirty nodes, LRU eviction): holds recently modified nodes.
2. `nodeCache` (clean cache): recently read unmodified nodes.
3. `baseDB` prefix `0x02`: persisted nodes.

Eviction: batch-evict oldest `evictionBatchSize` bytes from `writeBuffer` to disk.

### 4.7 Hashing

```
Leaf node:     hash = Hash256([value])
Internal node: hash = Hash256([version][token0][hash0]...[tokenN][hashN])
Root ID = hash(root node)
```

After `CommitToDB()`, hashes are recomputed bottom-up for modified subtrees. `RootGenConcurrency` goroutines parallelize hash computation.

**Clean shutdown:** `cleanShutdownKey` set to 1 before shutdown. On unclean shutdown (value = 0), intermediate nodes may need rebuild.

### 4.8 Proof Types

**Inclusion/Exclusion Proof:**
```go
type Proof struct {
    Path  []ProofNode
    Key   Key
    Value maybe.Maybe[[]byte]
}

type ProofNode struct {
    Key         Key
    ValueOrHash maybe.Maybe[[]byte]  // value if small, hash if large
    Children    map[byte]ids.ID      // child node hashes
}
```

- **Inclusion proof:** `Path` reaches node where `Key == query key` with non-nil `Value`.
- **Exclusion proof:** `Path` reaches a node where the query key doesn't exist.
- Verification: recompute hashes from leaves to root; compare with expected root ID.

**Range Proof:**
```go
type RangeProof struct {
    StartProof *Proof
    EndProof   *Proof
    KeyChanges []KeyChange  // all keys in [start, end], sorted
    KeyValues  map[string][]byte
}
```

Proves completeness: no keys exist in `[start, end]` that are not in `KeyChanges`.

**Change Proof:**
```go
type ChangeProof struct {
    StartProof *Proof     // lower bound in old state
    EndProof   *Proof     // upper bound in new state
    KeyChanges []KeyChange // [{key, oldValue, newValue}]
}
```

Proves differences between `startRootID` and `endRootID`. Enables incremental state sync.

### 4.9 History

```go
type trieHistory struct {
    changes []*changelist  // circular buffer, size = HistoryLength
}
```

Stores the `HistoryLength` (default 300) most recent change lists. Enables:
- `GetChangeProof(startRoot, endRoot)` without re-scanning state.
- `GetRangeProofAtRoot(rootID, start, end)` for historical queries.

### 4.10 State Synchronization

`database/merkle/sync/` implements incremental state sync:
1. Requesting node asks for key ranges + proofs.
2. Serving node returns `RangeProof`.
3. Requesting node validates proof and commits.
4. Repeat with next range until complete.

Uses `CommitRangeProof()` which returns the next key after the proof's end for pagination.

---

## 5. ArchiveDB (`x/archivedb/`)

Append-only historical state storage.

### 5.1 Key Encoding

```
Key:   [1-byte prefix][8-byte height big-endian][user key bytes]
Value: [1-bit operation][remaining bytes]
         0 = Put (value follows)
         1 = Delete (no value)
```

### 5.2 Operations

```go
func (db *Database) NewBatch(height uint64) *batch
// → batch.Put(key, value)
// → batch.Delete(key)
// → batch.Write()  // atomic flush

func (db *Database) Open(height uint64) *Reader
// → reader.Get(key)       // value at this height
// → reader.GetHeight(key) // (value, firstSetHeight)
```

**Read semantics:** `Get(key)` at height H searches backward from H to find the most recent entry with `height ≤ H`. Returns `ErrNotFound` if the key was deleted or never set.

**Example:**
```
Put("foo", "v1") at height 10
Put("foo", "v2") at height 100
Delete("foo")    at height 1000

Open(50).Get("foo")   → "v1"
Open(100).Get("foo")  → "v2"
Open(1000).Get("foo") → ErrNotFound
Open(99).GetHeight("foo") → ("v1", 10)
```

### 5.3 Characteristics

- No indexing overhead for heights without changes.
- Only deltas stored, not full snapshots.
- Use: blockchain state archival, forensic queries, historical API endpoints.

---

## 6. BlockDB (`x/blockdb/`)

Height-indexed block storage optimized for append-only writes and O(1) random access.

### 6.1 File Layout

```
blockdb.idx            — index file (random access by height)
blockdb_0.dat          — first data file
blockdb_1.dat          — second data file (when first exceeds MaxDataFileSize)
...
```

### 6.2 Index File Format

```go
type indexFileHeader struct {
    Version         uint64
    MaxDataFileSize uint64
    MinHeight       BlockHeight
    MaxHeight       BlockHeight
    NextWriteOffset uint64
    Reserved        [24]byte
}

type indexEntry struct {
    Offset   uint64   // byte offset in data file
    Size     uint32   // block data bytes
    Reserved [4]byte
}
```

Height → `(dataFileIndex, offset, size)` via `indexEntry` array. O(1) lookup.

### 6.3 Block Entry Format

```go
type blockEntryHeader struct {
    Height   BlockHeight
    Size     uint32
    Checksum uint64   // xxHash
    Version  uint16
}
// Followed by: [zstd-compressed block bytes]
```

- xxHash checksum on every block; corruption detected on read.
- Zstandard compression; configurable level.

### 6.4 Operations

```go
Put(height uint64, data []byte) error  // write block, update index
Get(height uint64) ([]byte, error)     // index lookup → read → decompress → verify
Sync(start, end uint64) error          // flush range to disk
```

### 6.5 Caching

LRU cache of open file handles (configurable `MaxOpenFiles`). Prevents repeated open/close of frequently-accessed data files.

### 6.6 Error Handling

`ErrCorrupted` returned on:
- Checksum mismatch.
- Invalid height in entry.
- Malformed header bytes.

Supports index rebuild from valid blocks for partial recovery.

---

## 7. Design Principles

1. **Interface segregation**: Small, composable interfaces; callers only depend on what they use.
2. **Atomic batching**: `Batch.Write()` is always all-or-nothing.
3. **Resource safety**: `Iterator.Release()` and `Database.Close()` always safe to call.
4. **Layered composition**: VersionDB → PrefixDB → LevelDB is a typical stack; each layer concerns one responsibility.
5. **Error propagation**: Iterators accumulate errors; checked via `it.Error()` after loop.
6. **Merkle consistency**: Root ID cryptographically commits to all state.
7. **History tracking**: MerkleDB's `trieHistory` enables proof generation without full state re-scan.
