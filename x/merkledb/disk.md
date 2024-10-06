# MerkleDB Direct on Disk

The MerkleDB was created using a generic KV database store as a backend (typically LevelDB). This project is to replace the usage of LevelDB with a direct on-disk store that leverages the merkle trie's own structure to layout the data on disk.

This is most similar to a B+ tree design + free list (LMDB / LMDBX), where the merkle trie's actual structure replaces the B+ tree.

## Original MerkleDB Design

### Design

The MerkleDB currently splits the merkle trie into intermediate nodes (branch nodes) and value nodes and keeps them in distinct sections of the database. When the MerkleDB commits a new trie view, it atomically writes all of the value nodes to disk and adds all intermediate nodes to the intermediate node database. Since the merkle trie's key-value pairs provides a canonical representation of the merkle trie even without the intermediate nodes, MerkleDB does not guarantee that all intermediate nodes are written to disk. This optimization reduces the amount of database operations required during normal operation. When the MerkleDB closes, it attempts to flush the latest version of all intermediate nodes to disk. If it fails to do so, the intermediaten node database can be left in an inconsistent state, which requires rebuilding the full merkle trie from the key-value pairs in the value database.

## Direct On-Disk

## Shared Interface

I've decoupled `x/merkledb` from its usage of key value database, so that you can focus on implementing a single interface in [disk.go](./disk.go).

The existing implementation splits merkle trie nodes into two separate sections of the database: intermediate nodes and value nodes. You can find the implementation for this in [disk.go](./disk.go) as well.

## Implementing the Shared Interface

To implement the direct on-disk trie, you can break down the work in the following order:

1. Implement on-disk serialization that includes pointers to child nodes
2. Complete the raw disk implementation without worrying about persistence (WAL) or garbage collection when nodes are deleted
3. Implement a free list to manage storage space within the file
4. Handle deleted nodes in `writeChanges` by adding them to the free list
5. Switch from always writing new nodes to the end of the file to allocating from the free list
6. Implement WAL for consistency
7. Implement a benchmark of the system (can send you the one we've used for our Rust implementation's benchmarks) and profile your implementation
8. Make it faster!


There are many potential optimizations! To name a handful: io_uring, prefetching, amortize commits, parallel prefetching tied to commit strategy, multi-dimensional caching, and optimizing the page layout to reduce required storage operations.

But, it's much better to start with a benchmark/profile and identify hot spots to decide where to optimize.
