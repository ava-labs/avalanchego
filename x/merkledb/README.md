# Path Based Merkelized Radix Trie

## TODOs

- [ ] Remove special casing around the root node from the physical structure of the hashed tree.
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
| Key: 0x91                         |  prefix of the key, representing the location of the node in the trie
| Value: 0x00                       |  the value, if one exists, that is stored at the key (keyPrefix + compressedKey)
| Children:                         |  a map of children node ids for any nodes in the trie that have this node's key as a prefix
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
| Child compressed key length (varint)              |
+----------------------------------------------------+
| Child compressed key (variable length bytes)      |
+----------------------------------------------------+
| Child ID (32 bytes)                                |
+----------------------------------------------------+
| Child has value (1 bytes)                          |
+----------------------------------------------------+
| Child index (varint)                               |
+----------------------------------------------------+
| Child compressed key length (varint)              |
+----------------------------------------------------+
| Child compressed key (variable length bytes)      |
+----------------------------------------------------+
| Child ID (32 bytes)                                |
+----------------------------------------------------+
| Child has value (1 bytes)                          |
+----------------------------------------------------+
|...                                                 |
+----------------------------------------------------+
```

Where:
* `Value existence flag` is `1` if this node has a value, otherwise `0`.
* `Value length` is the length of the value, if it exists (i.e. if `Value existence flag` is `1`.) Otherwise not serialized.
* `Value` is the value, if it exists (i.e. if `Value existence flag` is `1`.) Otherwise not serialized.
* `Number of children` is the number of children this node has.
* `Child index` is the index of a child node within the list of the node's children.
* `Child compressed key length` is the length of the child node's compressed key.
* `Child compressed key` is the child node's compressed key.
* `Child ID` is the child node's ID.
* `Child has value` indicates if that child has a value.

For each child of the node, we have an additional:

```
+----------------------------------------------------+
| Child index (varint)                               |
+----------------------------------------------------+
| Child compressed key length (varint)              |
+----------------------------------------------------+
| Child compressed key (variable length bytes)      |
+----------------------------------------------------+
| Child ID (32 bytes)                                |
+----------------------------------------------------+
| Child has value (1 bytes)                          |
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
The first is at child index `0`, has compressed key `0x01` and ID (in hex) `0x579eb3718a7e437d2ddce931ac7cc05a0bc695a9c2084f5df12fb96ad0fa3266`.
The second is at child index `14`, has compressed key `0x0F0F0F` and ID (in hex) `0x9845893c4f9d92c4e097fcf2589bc9d6882b1f18d1c2fc91d7df1d3fcbdb4238`.

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
| Child compressed key length (varint)                              |
| 0x02                                                               |
+--------------------------------------------------------------------+
| Child compressed key (variable length bytes)                      |
| 0x10                                                               |
+--------------------------------------------------------------------+
| Child ID (32 bytes)                                                |
| 0x579EB3718A7E437D2DDCE931AC7CC05A0BC695A9C2084F5DF12FB96AD0FA3266 |
+--------------------------------------------------------------------+
| Child index (varint)                                               |
| 0x0E                                                               |
+--------------------------------------------------------------------+
| Child compressed key length (varint)                              |
| 0x06                                                               |
+--------------------------------------------------------------------+
| Child compressed key (variable length bytes)                      |
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
* `Value length` is the length of the value, if it exists (i.e. if `Value existence flag` is `1`.) Otherwise not serialized.
* `Value` is the value, if it exists (i.e. if `Value existence flag` is `1`.) Otherwise not serialized.
* `Key length` is the number of nibbles in this node's key.
* `Key` is the node's key.

Note that, as with the node serialization format, the `Child index` values aren't necessarily sequential, but they are unique and strictly increasing.
Also like the node serialization format, there can be up to 16 blocks of children data.
However, note that child compressed keys are not included in the node ID calculation.

Once this is encoded, we `sha256` hash the resulting bytes to get the node's ID.

### Encoding Varints and Bytes

Varints are encoded with `binary.PutUvarint` from the standard library's `binary/encoding` package.
Bytes are encoded by simply copying them onto the buffer.

## Design choices

### []byte copying
Nodes contain a []byte which represents its value.  This slice should never be edited internally.  This allows usage without having to make copies of it for safety.
Anytime these values leave the library, for example in `Get`, `GetValue`, `GetProof`, `GetRangeProof`, etc, they need to be copied into a new slice to prevent
edits made outside the library from being reflected in the DB/TrieViews.

### Split Node Storage
The nodes are stored under two different prefixes depending on if the node contains a value.  
If it does contain a value it is stored within the ValueNodeDB and if it doesn't it is stored in the IntermediateNodeDB.
By splitting the nodes up by value, it allows better key/value iteration and a more compact key format.

### Single node type

A `Merkle Node` holds the IDs of its children, its value, as well as any key extension. This simplifies some logic and allows all of the data about a node to be loaded in a single database read. This trades off a small amount of storage efficiency (some fields may be `nil` but are still stored for every node).

### Validity

A `trieView` is built atop another trie, and there may be other `trieView`s built atop the same trie. We call these *siblings*. If one sibling is committed to database, we *invalidate* all other siblings and their descendants. Operations on an invalid trie return `ErrInvalid`. The children of the committed `trieView` are updated so that their new `parentTrie` is the database.

### Locking

`merkleDB` has a `RWMutex` named `lock`. Its read operations don't store data in a map, so a read lock suffices for read operations.
`merkleDB` has a `Mutex` named `commitLock`. It enforces that only a single view/batch is attempting to commit to the database at one time.  `lock` is insufficient because there is a period of view preparation where read access should still be allowed, followed by a period where a full write lock is needed. The `commitLock` ensures that only a single goroutine makes the transition from read => write.

A `trieView` is built atop another trie, which may be the underlying `merkleDB` or another `trieView`.
We use locking to guarantee atomicity/consistency of trie operations.

`trieView` has a `RWMutex` named `commitLock` which ensures that we don't create a view atop the `trieView` while it's being committed.
It also has a `RWMutex` named `validityTrackingLock` that is held during methods that change the view's validity, tracking of child views' validity, or of the `trieView` parent trie.  This lock ensures that writing/reading from `trieView` or any of its descendants is safe.
The `CommitToDB` method grabs the `merkleDB`'s `commitLock`. This is the only `trieView` method that modifies the underlying `merkleDB`.

In some of `merkleDB`'s methods, we create a `trieView` and call unexported methods on it without locking it.
We do so because the exported counterpart of the method read locks the `merkleDB`, which is already locked.
This pattern is safe because the `merkleDB` is locked, so no data under the view is changing, and nobody else has a reference to the view, so there can't be any concurrent access.

To prevent deadlocks, `trieView` and `merkleDB` never acquire the `commitLock` of descendant views.
That is, locking is always done from a view toward to the underlying `merkleDB`, never the other way around.
The `validityTrackingLock` goes the opposite way. A view can lock the `validityTrackingLock` of its children, but not its ancestors. Because of this, any function that takes the `validityTrackingLock` must not take the `commitLock` as this may cause a deadlock. Keeping `commitLock` solely in the ancestor direction and `validityTrackingLock` solely in the descendant direction prevents deadlocks from occurring.
