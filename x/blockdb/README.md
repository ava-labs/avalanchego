# BlockDB

BlockDB is a specialized database optimized for blockchain blocks.

## Key Functionalities

- **O(1) Performance**: Both reads and writes complete in constant time
- **Parallel Operations**: Multiple threads can read and write blocks concurrently without blocking
- **Flexible Write Ordering**: Supports out-of-order block writes for bootstrapping
- **Configurable Durability**: Optional `syncToDisk` mode guarantees immediate recoverability
- **Automatic Recovery**: Detects and recovers unindexed blocks after unclean shutdowns
- **Block Compression**: zstd compression for block data
- **In-Memory Cache**: LRU cache for recently accessed blocks

## Design

BlockDB uses a single index file and multiple data files. The index file maps block heights to locations in the data files, while data files store the actual block content. Data storage can be split across multiple data files based on the maximum data file size.

```
┌─────────────────┐         ┌─────────────────┐
│   Index File    │         │  Data File 1    │
│   (.idx)        │         │   (.dat)        │
├─────────────────┤         ├─────────────────┤
│ Header          │         │ Block 0         │
│ - Version       │  ┌─────>│ - Header        │
│ - Min Height    │  │      │ - Data          │
│ - Max Height    │  │      ├─────────────────┤
│ - Data Size     │  │      │ Block 1         │
│ - ...           │  │  ┌──>│ - Header        │
├─────────────────┤  │  │   │ - Data          │
│ Entry[0]        │  │  │   ├─────────────────┤
│ - Offset ───────┼──┘  │   │     ...         │
│ - Size          │     │   └─────────────────┘
│ - Header Size   │     │
├─────────────────┤     │
│ Entry[1]        │     │
│ - Offset ───────┼─────┘
│ - Size          │
│ - Header Size   │
├─────────────────┤
│     ...         │
└─────────────────┘
```

### File Formats

#### Index File Structure

The index file consists of a fixed-size header followed by fixed-size entries:

```
Index File Header (64 bytes):
┌────────────────────────────────┬─────────┐
│ Field                          │ Size    │
├────────────────────────────────┼─────────┤
│ Version                        │ 8 bytes │
│ Max Data File Size             │ 8 bytes │
│ Min Block Height               │ 8 bytes │
│ Max Block Height               │ 8 bytes │
│ Next Write Offset              │ 8 bytes │
│ Reserved                       │ 24 bytes│
└────────────────────────────────┴─────────┘

Index Entry (16 bytes):
┌────────────────────────────────┬─────────┐
│ Field                          │ Size    │
├────────────────────────────────┼─────────┤
│ Data File Offset               │ 8 bytes │
│ Block Data Size                │ 4 bytes │
│ Reserved                       │ 4 bytes │
└────────────────────────────────┴─────────┘
```

#### Data File Structure

Each block in the data file is stored with a block entry header followed by the raw block data:

```
Block Entry Header (22 bytes):
┌────────────────────────────────┬─────────┐
│ Field                          │ Size    │
├────────────────────────────────┼─────────┤
│ Height                         │ 8 bytes │
│ Size                           │ 4 bytes │
│ Checksum                       │ 8 bytes │
│ Version                        │ 2 bytes │
└────────────────────────────────┴─────────┘
```

### Block Overwrites

BlockDB allows overwriting blocks at existing heights. When a block is overwritten, the new block is appended to the data file and the index entry is updated to point to the new location, leaving the old block data as unreferenced "dead" space. However, since blocks are immutable and rarely overwritten (e.g., during reorgs), this trade-off should have minimal impact in practice.

### Fixed-Size Index Entries

Each index entry is exactly 16 bytes on disk, containing the offset, size, and reserved bytes for future use. This fixed size enables direct calculation of where each block's index entry is located, providing O(1) lookups. For blockchains with high block heights, the index remains efficient, even at height 1 billion, the index file would only be ~16GB.

### Durability and Fsync Behavior

BlockDB provides configurable durability through the `syncToDisk` parameter:

**Data File Behavior:**

- **When `syncToDisk=true`**: The data file is fsync'd after every block write, guaranteeing durability against both process failures and kernel/machine failures.
- **When `syncToDisk=false`**: Data file writes are buffered, providing durability against process failures but not against kernel or machine failures.

**Index File Behavior:**

- **When `syncToDisk=true`**: The index file is fsync'd every `CheckpointInterval` blocks (when the header is written).
- **When `syncToDisk=false`**: The index file relies on OS buffering and is not explicitly fsync'd.

### Recovery Mechanism

On startup, BlockDB checks for signs of an unclean shutdown by comparing the data file size on disk with the indexed data size stored in the index file header. If the data files are larger than what the index claims, it indicates that blocks were written but the index wasn't properly updated before shutdown.

**Recovery Process:**

1. Starts scanning from where the index left off (`NextWriteOffset`)
2. For each unindexed block found:
   - Validates the block entry header and checksum
   - Writes the corresponding index entry
3. Calculates the max block height
4. Updates the index header with the updated max block height and next write offset

### Single-Process Access

BlockDB does not support concurrent access from multiple processes. It acquires an exclusive advisory file lock on a `LOCK` file at open time; a second process attempting to open the same database fails immediately. Within a single process, the database is safe to use from multiple goroutines.

`IndexDir` and `DataDir` must be dedicated to BlockDB. Sharing folders with other databases risks filename conflicts (e.g., the conventional `LOCK` file).

**Locked paths.** A `LOCK` file is created and locked in `IndexDir`, and (when distinct) in `DataDir`.

**Lifecycle.** The lock is acquired before any database files are opened or recovery is attempted, and released after all files are closed. If the process exits unexpectedly (panic, `SIGKILL`, OOM kill), the OS releases the lock automatically.

**Limits.** The lock is advisory and only coordinates between cooperating processes. It does not prevent non-cooperating tools (`rm`, backup utilities, editors) from modifying or deleting the database files. Deleting the `LOCK` file while a database is open breaks the protection.

## Usage

### Creating a Database

```go
import (
    "errors"
    "github.com/ava-labs/avalanchego/x/blockdb"
)

config := blockdb.DefaultConfig().
    WithDir("/path/to/blockdb")
db, err := blockdb.New(config, logging.NoLog{})
if err != nil {
    fmt.Println("Error creating database:", err)
    return
}
defer db.Close()
```

### Writing and Reading Blocks

```go
// Write a block
height := uint64(100)
blockData := []byte("block data")
err := db.Put(height, blockData)
if err != nil {
    fmt.Println("Error writing block:", err)
    return
}

// Read a block
blockData, err := db.Get(height)
if err != nil {
    if errors.Is(err, database.ErrNotFound) {
        fmt.Println("Block doesn't exist at this height")
        return
    }
    fmt.Println("Error reading block:", err)
    return
}
```

## TODO

- Use a buffered pool to avoid allocations on reads and writes
- Add performance benchmarks
- Consider supporting missing data files (currently we error if any data files are missing)
