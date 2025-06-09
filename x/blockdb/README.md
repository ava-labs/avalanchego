# BlockDB

BlockDB is a specialized storage system designed for blockchain blocks. It provides O(1) write performance with support for parallel operations. Unlike general-purpose key-value stores like LevelDB that require periodic compaction, BlockDB's append-only design ensures consistently fast writes without the overhead of background maintenance operations.

## Key Functionalities (Needs Review)

- **O(1) Performance**: Both reads and writes complete in constant time
- **Parallel Operations**: Multiple threads can read and write blocks concurrently without blocking
- **Flexible Write Ordering**: Supports out-of-order block writes for efficient synchronization
- **Configurable Durability**: Optional `syncToDisk` mode guarantees immediate recoverability at the cost of performance
- **Automatic Recovery**: Detects and recovers unindexed blocks after unclean shutdowns
- **Data Integrity**: Checksums verify block data on every read
- **No Maintenance Required**: Append-only design eliminates the need for compaction or reorganization
- **Progress Tracking**: Maintains maximum contiguous height for sync status

## Architecture

BlockDB uses two file types: index files and data files. The index file maps block heights to locations in data files, while data files store the actual block content. Data storage can be split across multiple files based on size limits.

```
┌─────────────────┐         ┌─────────────────┐
│   Index File    │         │  Data File 1    │
│   (.idx)        │         │   (.dat)        │
├─────────────────┤         ├─────────────────┤
│ Header          │         │ Block 1         │
│ - Version       │  ┌─────>│ - Header        │
│ - Min Height    │  │      │ - Data          │
│ - MCH           │  │      ├─────────────────┤
│ - Data Size     │  │      │ Block 2         │
├─────────────────┤  │  ┌──>│ - Header        │
│ Entry[0]        │  │  │   │ - Data          │
│ - Offset ───────┼──┘  │   ├─────────────────┤
│ - Size          │     │   │     ...         │
├─────────────────┤     │   └─────────────────┘
│ Entry[1]        │     │
│ - Offset ───────┼─────┘   ┌─────────────────┐
│ - Size          │         │  Data File 2    │
├─────────────────┤         │   (.dat)        │
│     ...         │         ├─────────────────┤
└─────────────────┘         │ Block N         │
                            │ - Header        │
                            │ - Data          │
                            ├─────────────────┤
                            │     ...         │
                            └─────────────────┘
```

## Implementation Details

### File Formats

#### Index File Structure

The index file consists of a fixed-size header followed by fixed-size entries:

```
Index File Header (48 bytes):
┌────────────────────────────────┬─────────┐
│ Field                          │ Size    │
├────────────────────────────────┼─────────┤
│ Version                        │ 8 bytes │
│ Max Data File Size             │ 8 bytes │
│ Max Block Height               │ 8 bytes │
│ Min Block Height               │ 8 bytes │
│ Max Contiguous Height          │ 8 bytes │
│ Data File Size                 │ 8 bytes │
└────────────────────────────────┴─────────┘

Index Entry (16 bytes):
┌────────────────────────────────┬─────────┐
│ Field                          │ Size    │
├────────────────────────────────┼─────────┤
│ Data File Offset               │ 8 bytes │
│ Block Data Size                │ 8 bytes │
└────────────────────────────────┴─────────┘
```

#### Data File Structure

Each block in the data file is stored with a header followed by the raw block data:

```
Block Header (24 bytes):
┌────────────────────────────────┬─────────┐
│ Field                          │ Size    │
├────────────────────────────────┼─────────┤
│ Height                         │ 8 bytes │
│ Size                           │ 8 bytes │
│ Checksum                       │ 8 bytes │
└────────────────────────────────┴─────────┘
```

### Design Decisions

#### Append-Only Architecture

BlockDB is strictly append-only with no support for deletions. This aligns with blockchain's immutable nature and provides:

- Simplified concurrency model
- Predictable write performance
- Straightforward recovery logic
- No compaction overhead

**Trade-off**: Overwriting a block leaves the old data as unreferenced "dead" space. However, since blockchain blocks are immutable and rarely overwritten (only during reorgs), this trade-off has minimal impact in practice.

#### Fixed-Size Index Entries

Each index entry is exactly 16 bytes, containing the offset and size. This fixed size enables direct calculation of where each block's index entry is located, providing O(1) lookups. For blockchains with high block heights, the index remains efficient - even at height 1 billion, the index file would only be ~16GB.

#### Two File Type Separation

Separating index and data provides several benefits:

- Index files remain relatively small and can benefit from SSD storage
- Data files can use cheaper storage and be backed up independently
- Sequential append-only writes to data files minimize fragmentation
- Index can be rebuilt by scanning data files if needed

#### Out-of-Order Block Writing

Blocks can be written at any height regardless of arrival order. This is essential for blockchain nodes that may receive blocks out of sequence during syncing operations.

#### Durability and Fsync Behavior

BlockDB provides configurable durability through the `syncToDisk` parameter:

- When enabled, the data file is fsync'd after every block write, guaranteeing immediate durability
- The index file is fsync'd periodically (every `CheckpointInterval` blocks) to balance performance and recovery time
- When disabled, writes rely on OS buffering, trading durability for significantly better performance

### Key Operations

#### Write Performance

- **Time Complexity**: O(1) to write a block
- **I/O Pattern**: Sequential append to data file + single index entry write
- **Block Size Impact**: While index operations are O(1), total write time depends on block size. With a maximum block size enforced, write time remains bounded, maintaining effectively O(1) performance.

#### Read Performance

- **Time Complexity**: O(1) to read a block
- **I/O Pattern**: One index read + one data read
- **Concurrency**: Multiple blocks can be read in parallel

#### Recovery Mechanism

On startup, BlockDB checks for signs of an unclean shutdown. If detected, it performs recovery:

1. Compares the data file size with the indexed data size (stored in index header)
2. If data file is larger, starts scanning from where the index left off
3. For each unindexed block found:
   - Validates block header and checksum
   - Writes the corresponding index entry
4. Updates maximum contiguous height
5. Persists the updated index header

### Concurrency Model

BlockDB uses a reader-writer lock for overall thread safety, with atomic operations for write coordination:

- Multiple threads can read different blocks simultaneously without blocking
- Multiple threads can write concurrently - they use atomic operations to allocate unique space in the data file
- The reader-writer lock ensures consistency between reads and writes

## Usage

### Creating a Store

```go
import "github.com/ava-labs/avalanchego/x/blockdb"

opts := blockdb.DefaultStoreOptions()
opts.MinimumHeight = 1

store, err := blockdb.NewStore(
    "/path/to/index",  // Index directory
    "/path/to/data",   // Data directory
    true,              // Sync to disk
    false,             // Don't truncate existing data
    opts,
    logger,
)
if err != nil {
    return err
}
defer store.Close()
```

### Writing and Reading Blocks

```go
// Write a block
height := uint64(100)
blockData := []byte("block data...")
err := store.WriteBlock(height, blockData)

// Read a block
blockData, err := store.ReadBlock(height)
if err == blockdb.ErrBlockNotFound {
    // Block doesn't exist at this height
}

// Query store state
maxContiguous := store.MaxContiguousHeight()
minHeight := store.MinHeight()
```

## TODO

- [ ] **Multiple Data Files**: Split data across multiple files when MaxDataFileSize is reached
- [ ] **Block Cache**: Implement circular buffer cache for recently accessed blocks
- [ ] **Enforced In-Order Writes**: Optional mode to require blocks be written sequentially, preventing gaps
- [ ] **User buffered pool**: Use a buffered pool for fetch index entries and block headers to avoid allocations
- [ ] **Unit Tests**: Add comprehensive test coverage for all core functionality
- [ ] **Benchmarks**: Add performance benchmarks for all major operations
