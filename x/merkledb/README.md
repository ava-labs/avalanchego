# Path Based Merkelized Radix Trie

## TODOs

- [ ] Remove special casing around the root node from the physical structure of the hashed tree.
- [ ] Analyze performance impact of needing to skip intermediate nodes when generating range and change proofs
  - [ ] Consider moving nodes with values to a separate db prefix
- [ ] Analyze performance of using database snapshots rather than in-memory history
- [ ] Improve intermediate node regeneration after ungraceful shutdown by reusing successfully written subtrees

## Introduction

The Merkle Trie is a data structure that allows efficient and secure verification of the contents. It is a combination of a [Merkle Tree](https://en.wikipedia.org/wiki/Merkle_tree) and a [Radix Trie](https://en.wikipedia.org/wiki/Radix_tree).

The trie contains `Merkle Nodes`, which store key/value and children information.

Each `Merkle Node` represents a key path into the trie. It stores the key, the value (if one exists), its ID, and the IDs of its children nodes. The children have keys that contain the current node's key path as a prefix, and the index of each child indicates the next nibble in that child's key. For example, if we have two nodes, Node 1 with key path `0x91A` and Node 2 with key path `0x91A4`, Node 2 is stored in index `0x4` of Node 1's children (since 0x4 is the first value after the common prefix).

To reduce the depth of nodes in the trie, a `Merkle Node` utilizes path compression. Instead of having a long chain of nodes each containing only a single nibble of the key, we can "compress" the path by recording additional key information with each of a node's children. For example, if we have three nodes, Node 1 with key path `0x91A`, Node 2 with key path `0x91A4`, and Node 3 with key path `0x91A5132`, then Node 1 has a key of `0x91A`. Node 2 is stored at index `0x4` of Node 1's children since `4` is the next nibble in Node 2's key after skipping the common nibbles from Node 1's key. Node 3 is stored at index `0x5` of Node 1's children. Rather than have extra nodes for the remainder of Node 3's key, we instead store the rest of the key (`132`) in Node 1's children info.

```
+-----------------------------------+
| Merkle Node                       | 
|                                   |
| ID: 0x0131                        |  an id representing the current node, derived from the node's value and all children ids
| Key: 0x91                         |  prefix of the key path, representing the location of the node in the trie
| Value: 0x00                       |  the value, if one exists, that is stored at the key path (pathPrefix + compressedPath)
| Children:                         |  a map of children node ids for any nodes in the trie that have this node's key path as a prefix 
|   0: [:0x00542F]                  |  child 0 represents a node with key 0x910 with ID 0x00542F
|   1: [0x432:0xA0561C]             |  child 1 represents a node with key 0x911432 with ID 0xA0561C
|   ...                             |
|   15: [0x9A67B:0x02FB093]         |  child 15 represents a node with key 0x91F9A67B with ID 0x02FB093
+-----------------------------------+ 
```

## Design choices

### []byte copying
Nodes contain a []byte which represents its value.  This slice should never be edited internally.  This allows usage without having to make copies of it for safety.
Anytime these values leave the library, for example in `Get`, `GetValue`, `GetProof`, `GetRangeProof`, etc, they need to be copied into a new slice to prevent
edits made outside of the library from being reflected in the DB/TrieViews.

### Single node type

A `Merkle Node` holds the IDs of its children, its value, as well as any path extension. This simplifies some logic and allows all of the data about a node to be loaded in a single database read. This trades off a small amount of storage efficiency (some fields may be `nil` but are still stored for every node).

### Validity

A `trieView` is built atop another trie, and that trie could change at any point.  If it does, all descendants of the trie will be marked invalid before the edit of the trie occurs.  If an operation is performed on an invalid trie, an ErrInvalid error will be returned instead of the expected result.  When a view is committed, all of its sibling views (the views that share the same parent) are marked invalid and any child views of the view have their parent updated to exclude any committed views between them and the db.

### Locking

`Database` has a `RWMutex` named `lock`. Its read operations don't store data in a map, so a read lock suffices for read operations.
`Database` has a `Mutex` named `commitLock`.  It enforces that only a single view/batch is attempting to commit to the database at one time.  `lock` is insufficient because there is a period of view preparation where read access should still be allowed, followed by a period where a full write lock is needed.  The `commitLock` ensures that only a single goroutine makes the transition from read->write.

A `trieView` is built atop another trie, which may be the underlying `Database` or another `trieView`.
It's important to guarantee atomicity/consistency of trie operations.
That is, if a view method is executing, the views/database underneath the view shouldn't be changing.
To prevent this, we need to use locking.

`trieView` has a `RWMutex` named `lock` that's held when methods that access the trie's structure are executing.  It is responsible for ensuring that writing/reading from a `trieView` or from any *ancestor* is safe.
It also has a `RWMutex` named `validityTrackingLock` that is held during methods that change the view's validity, tracking of child views' validity, or of the `trieView` parent trie.  This lock ensures that writing/reading from `trieView` or any of its *descendants* is safe.
The `Commit` function also grabs the `Database`'s `commitLock` lock. This is the only `trieView` method that modifies the underlying `Database`.  If an ancestor is modified during this time, the commit will error with ErrInvalid.

In some of `Database`'s methods, we create a `trieView` and call unexported methods on it without locking it.
We do so because the exported counterpart of the method read locks the `Database`, which is already locked.
This pattern is safe because the `Database` is locked, so no data under the view is changing, and nobody else has a reference to the view, so there can't be any concurrent access.  

To prevent deadlocks, `trieView` and `Database` never acquire the `lock` of any descendant views that are built atop it.
That is, locking is always done from a view down to the underlying `Database`, never the other way around.
The `validityTrackingLock` goes the opposite way.  Views can validityTrackingLock their children, but not their ancestors. Because of this, any function that takes the `validityTrackingLock` should avoid taking the `lock` as this will likely trigger a deadlock.  Keeping `lock` solely in the ancestor direction and `validityTrackingLock` solely in the descendant direction prevents deadlocks from occurring.  
