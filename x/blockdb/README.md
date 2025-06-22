# BlockDB

BlockDB is a specialized database optimized for blockchain blocks.

## Key Functionalities

- **O(1) Performance**: Both reads and writes complete in constant time
- **Parallel Operations**: Multiple threads can read and write blocks concurrently without blocking
- **Flexible Write Ordering**: Supports out-of-order block writes for bootstrapping
- **Configurable Durability**: Optional `syncToDisk` mode guarantees immediate recoverability
- **Automatic Recovery**: Detects and recovers unindexed blocks after unclean shutdowns

## Design

BlockDB uses two file types: index files and data files. The index file maps block heights to locations in data files, while data files store the actual block content. Data storage can be split across multiple files based on the maximum data file size.

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
│ - ...           │  │      │ - Header        │
├─────────────────┤  │  ┌──>│ - Data          │
│ Entry[0]        │  │  │   ├─────────────────┤
│ - Offset ───────┼──┘  │   │     ...         │
│ - Size          │     │   └─────────────────┘
│ - Header Size   │     │
├─────────────────┤     │   ┌─────────────────┐
│ Entry[1]        │     │   │  Data File 2    │
│ - Offset ───────┼─────┘   │   (.dat)        │
│ - Size          │         ├─────────────────┤
│ - Header Size   │         │ Block N         │
├─────────────────┤         │ - Header        │
│     ...         │         │ - Data          │
└─────────────────┘         ├─────────────────┤
                            │     ...         │
                            └─────────────────┘
```

### File Formats

#### Index File Structure

The index file consists of a fixed-size header followed by fixed-size entries:

```
Index File Header (72 bytes):
┌────────────────────────────────┬─────────┐
│ Field                          │ Size    │
├────────────────────────────────┼─────────┤
│ Version                        │ 8 bytes │
│ Max Data File Size             │ 8 bytes │
│ Max Block Height               │ 8 bytes │
│ Min Block Height               │ 8 bytes │
│ Max Contiguous Height          │ 8 bytes │
│ Data File Size                 │ 8 bytes │
│ Reserved                       │ 24 bytes│
└────────────────────────────────┴─────────┘

Index Entry (18 bytes):
┌────────────────────────────────┬─────────┐
│ Field                          │ Size    │
├────────────────────────────────┼─────────┤
│ Data File Offset               │ 8 bytes │
│ Block Data Size                │ 8 bytes │
│ Header Size                    │ 2 bytes │
└────────────────────────────────┴─────────┘
```

#### Data File Structure

Each block in the data file is stored with a header followed by the raw block data:

```
Block Header (26 bytes):
┌────────────────────────────────┬─────────┐
│ Field                          │ Size    │
├────────────────────────────────┼─────────┤
│ Height                         │ 8 bytes │
│ Size                           │ 8 bytes │
│ Header Size                    │ 2 bytes │
│ Checksum                       │ 8 bytes │
└────────────────────────────────┴─────────┘
```

### Block Overwrites

BlockDB allows overwriting blocks at existing heights. When a block is overwritten, the new block is appended to the data file and the index entry is updated to point to the new location, leaving the old block data as unreferenced "dead" space. However, since blocks are immutable and rarely overwritten (e.g., during reorgs), this trade-off should have minimal impact in practice.

### Fixed-Size Index Entries

Each index entry is exactly 18 bytes on disk, containing the offset, size, and header size. This fixed size enables direct calculation of where each block's index entry is located, providing O(1) lookups. For blockchains with high block heights, the index remains efficient, even at height 1 billion, the index file would only be ~18GB.

### Durability and Fsync Behavior

BlockDB provides configurable durability through the `syncToDisk` parameter:

- When enabled, the data file is fsync'd after every block write, guaranteeing immediate durability
- The index file is fsync'd periodically (every `CheckpointInterval` blocks) to balance performance and recovery time
- When disabled, writes rely on OS buffering, trading durability for significantly better performance

### Recovery Mechanism

On startup, BlockDB checks for signs of an unclean shutdown. If detected, it performs recovery:

1. Compares the data file size with the indexed data size (stored in the index header)
2. If the data file is larger, it starts scanning from where the index left off
3. For each unindexed block found:
   - Validates the block header and checksum
   - Writes the corresponding index entry
4. Updates the max contiguous height and max block height
5. Persists the updated index header

## Usage

### Creating a Database

```go
import "github.com/ava-labs/avalanchego/x/blockdb"

config := blockdb.DefaultDatabaseOptions()
db, err := blockdb.New(
    "/path/to/index",  // Index directory
    "/path/to/data",   // Data directory
    true,              // Sync to disk
    false,             // Don't truncate existing data
    config,
    logger,
)
if err != nil {
    fmt.Println("Error creating database:", err)
    return
}
defer db.Close()
```

### Writing and Reading Blocks

```go
// Write a block with header size
height := uint64(100)
blockData := []byte("block data...")
headerSize := uint16(500) // First 500 bytes are the header
err := db.WriteBlock(height, blockData, headerSize)

// Read a complete block
blockData, err := db.ReadBlock(height)
if blockData == nil {
    // Block doesn't exist at this height
}

// Read block components separately
headerData, err := db.ReadHeader(height)
bodyData, err := db.ReadBody(height)
```

## TODO

- [ ] Compress data files to reduce storage size
- [ ] Split data across multiple files when `MaxDataFileSize` is reached
- [ ] Implement a block cache for recently accessed blocks
- [ ] Use a buffered pool to avoid allocations on reads and writes
- [ ] Add tests for core functionality
- [ ] Add performance benchmarks
