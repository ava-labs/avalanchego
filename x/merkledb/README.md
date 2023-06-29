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

## Serialization

### Node

Nodes are persisted in an underlying database. In order to persist nodes, we must first serialize them.
Serialization is done by the `encoder` interface defined in `codec.go`.

The node serialization format is as follows:

```
+----------------------------------------------------+
| Value existence flag (1 byte)                      |
+----------------------------------------------------+
| Value length (varint) (optional)                   |
+----------------------------------------------------+
| Value (variable length bytes) (optional)           |
+----------------------------------------------------+
| Number of children (varint)                        |
+----------------------------------------------------+
| Child index (varint)                               |
+----------------------------------------------------+
| Child compressed path length (varint)              |
+----------------------------------------------------+
| Child compressed path (variable length bytes)      |
+----------------------------------------------------+
| Child ID (32 bytes)                                |
+----------------------------------------------------+
| Child index (varint)                               |
+----------------------------------------------------+
| Child compressed path length (varint)              |
+----------------------------------------------------+
| Child compressed path (variable length bytes)      |
+----------------------------------------------------+
| Child ID (32 bytes)                                |
+----------------------------------------------------+
|...                                                 |
+----------------------------------------------------+
```

Where:
* `Value existence flag` is `1` if this node has a value, otherwise `0`.
* `Value length` is the length of the value, if it exists (i.e. if `Value existince flag` is `1`.) Otherwise not serialized.
* `Value` is the value, if it exists (i.e. if `Value existince flag` is `1`.) Otherwise not serialized.
* `Number of children` is the number of children this node has.
* `Child index` is the index of a child node within the list of the node's children.
* `Child compressed path length` is the length of the child node's compressed path.
* `Child compressed path` is the child node's compressed path.
* `Child ID` is the child node's ID.

For each child of the node, we have an additional:

```
+----------------------------------------------------+
| Child index (varint)                               |
+----------------------------------------------------+
| Child compressed path length (varint)              |
+----------------------------------------------------+
| Child compressed path (variable length bytes)      |
+----------------------------------------------------+
| Child ID (32 bytes)                                |
+----------------------------------------------------+
```

Note that the `Child index` are not necessarily sequential. For example, if a node has 3 children, the `Child index` values could be `0`, `2`, and `15`. 
However, the `Child index` values must be strictly increasing. For example, the `Child index` values cannot be `0`, `0`, and `1`, or `1`, `0`.

Since a node can have up to 16 children, there can be up to 16 such blocks of children data.

#### Example

Let's take a look at an example node. 

Its byte representation (in hex) is: `0x01020204000210579EB3718A7E437D2DDCE931AC7CC05A0BC695A9C2084F5DF12FB96AD0FA32660E06FFF09845893C4F9D92C4E097FCF2589BC9D6882B1F18D1C2FC91D7DF1D3FCBDB4238`

The node's key is empty (its the root) and has value `0x02`.
It has two children.
The first is at child index `0`, has compressed path `0x01` and ID (in hex) `0x579eb3718a7e437d2ddce931ac7cc05a0bc695a9c2084f5df12fb96ad0fa3266`.
The second is at child index `14`, has compressed path `0x0F0F0F` and ID (in hex) `0x9845893c4f9d92c4e097fcf2589bc9d6882b1f18d1c2fc91d7df1d3fcbdb4238`.

```
+--------------------------------------------------------------------+
| Value existence flag (1 byte)                                      |
| 0x01                                                               |
+--------------------------------------------------------------------+
| Value length (varint) (optional)                                   |
| 0x02                                                               |
+--------------------------------------------------------------------+
| Value (variable length bytes) (optional)                           |
| 0x02                                                               |
+--------------------------------------------------------------------+
| Number of children (varint)                                        |
| 0x04                                                               |
+--------------------------------------------------------------------+
| Child index (varint)                                               |
| 0x00                                                               |
+--------------------------------------------------------------------+
| Child compressed path length (varint)                              |
| 0x02                                                               |
+--------------------------------------------------------------------+
| Child compressed path (variable length bytes)                      |
| 0x10                                                               |
+--------------------------------------------------------------------+
| Child ID (32 bytes)                                                |
| 0x579EB3718A7E437D2DDCE931AC7CC05A0BC695A9C2084F5DF12FB96AD0FA3266 |
+--------------------------------------------------------------------+
| Child index (varint)                                               |
| 0x0E                                                               |
+--------------------------------------------------------------------+
| Child compressed path length (varint)                              |
| 0x06                                                               |
+--------------------------------------------------------------------+
| Child compressed path (variable length bytes)                      |
| 0xFFF0                                                             |
+--------------------------------------------------------------------+
| Child ID (32 bytes)                                                |
| 0x9845893C4F9D92C4E097FCF2589BC9D6882B1F18D1C2FC91D7DF1D3FCBDB4238 |
+--------------------------------------------------------------------+
```

### Node Hashing

Each node must have a unique ID that identifies it. This ID is calculated by hashing the following values:
* The node's children
* The node's value digest
* The node's key

Specifically, we encode these values in the following way:

```
+----------------------------------------------------+
| Number of children (varint)                        |
+----------------------------------------------------+
| Child index (varint)                               |
+----------------------------------------------------+
| Child ID (32 bytes)                                |
+----------------------------------------------------+
| Child index (varint)                               |
+----------------------------------------------------+
| Child ID (32 bytes)                                |
+----------------------------------------------------+
|...                                                 |
+----------------------------------------------------+
| Value existence flag (1 byte)                      |
+----------------------------------------------------+
| Value length (varint) (optional)                   |
+----------------------------------------------------+
| Value (variable length bytes) (optional)           |
+----------------------------------------------------+
| Key length (varint)                                |
+----------------------------------------------------+
| Key (variable length bytes)                        |
+----------------------------------------------------+
```

Where:
* `Number of children` is the number of children this node has.
* `Child index` is the index of a child node within the list of the node's children.
* `Child ID` is the child node's ID.
* `Value existence flag` is `1` if this node has a value, otherwise `0`.
* `Value length` is the length of the value, if it exists (i.e. if `Value existince flag` is `1`.) Otherwise not serialized.
* `Value` is the value, if it exists (i.e. if `Value existince flag` is `1`.) Otherwise not serialized.
* `Key length` is the number of nibbles in this node's key.
* `Key` is the node's key.

Note that, as with the node serialization format, the `Child index` values aren't necessarily sequential, but they are unique and strictly increasing.
Also like the node serialization format, there can be up to 16 blocks of children data.
However, note that child compressed paths are not included in the node ID calculation.

Once this is encoded, we `sha256` hash the resulting bytes to get the node's ID.

### Encoding Varints and Bytes

Varints are encoded with `binary.PutVarint` from the standard library's `binary/encoding` package.
Bytes are encoded by simply copying them onto the buffer.

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
`Database` has a `Mutex` named `commitLock`.  It enforces that only a single view/batch is attempting to commit to the database at one time.  `lock` is insufficient because there is a period of view preparation where read access should still be allowed, followed by a period where a full write lock is needed.  The `commitLock` ensures that only a single goroutine makes the transition from read => write.

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
