// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

const (
	progressLogFrequency       = 30 * time.Second
	atomicKeyLength            = wrappers.LongLen + common.HashLength
	sharedMemoryApplyBatchSize = 10_000 // specifies the number of atomic operations to batch progress updates

	atomicTrieTipBufferSize = 1 // No need to support a buffer of previously accepted tries for the atomic trie
	atomicTrieMemoryCap     = 64 * units.MiB
)

var (
	_                            AtomicTrie = &atomicTrie{}
	lastCommittedKey                        = []byte("atomicTrieLastCommittedBlock")
	appliedSharedMemoryCursorKey            = []byte("atomicTrieLastAppliedToSharedMemory")
	heightMapRepairKey                      = []byte("atomicTrieHeightMapRepair")
)

// AtomicTrie maintains an index of atomic operations by blockchainIDs for every block
// height containing atomic transactions. The backing data structure for this index is
// a Trie. The keys of the trie are block heights and the values (leaf nodes)
// are the atomic operations applied to shared memory while processing the block accepted
// at the corresponding height.
type AtomicTrie interface {
	// OpenTrie returns a modifiable instance of the atomic trie backed by trieDB
	// opened at hash.
	OpenTrie(hash common.Hash) (*trie.Trie, error)

	// UpdateTrie updates [tr] to inlude atomicOps for height.
	UpdateTrie(tr *trie.Trie, height uint64, atomicOps map[ids.ID]*atomic.Requests) error

	// Iterator returns an AtomicTrieIterator to iterate the trie at the given
	// root hash starting at [cursor].
	Iterator(hash common.Hash, cursor []byte) (AtomicTrieIterator, error)

	// LastCommitted returns the last committed hash and corresponding block height
	LastCommitted() (common.Hash, uint64)

	// TrieDB returns the underlying trie database
	TrieDB() *trie.Database

	// Root returns hash if it exists at specified height
	// if trie was not committed at provided height, it returns
	// common.Hash{} instead
	Root(height uint64) (common.Hash, error)

	// LastAcceptedRoot returns the most recent accepted root of the atomic trie,
	// or the root it was initialized to if no new tries were accepted yet.
	LastAcceptedRoot() common.Hash

	// InsertTrie updates the trieDB with the provided node set and adds a reference
	// to root in the trieDB. Once InsertTrie is called, it is expected either
	// AcceptTrie or RejectTrie be called for the same root.
	InsertTrie(nodes *trienode.NodeSet, root common.Hash) error

	// AcceptTrie marks root as the last accepted atomic trie root, and
	// commits the trie to persistent storage if height is divisible by
	// the commit interval. Returns true if the trie was committed.
	AcceptTrie(height uint64, root common.Hash) (bool, error)

	// RejectTrie dereferences root from the trieDB, freeing memory.
	RejectTrie(root common.Hash) error

	// RepairHeightMap repairs the height map of the atomic trie by iterating
	// over all leaves in the trie and committing the trie at every commit interval.
	RepairHeightMap(to uint64) (bool, error)
}

// AtomicTrieIterator is a stateful iterator that iterates the leafs of an AtomicTrie
type AtomicTrieIterator interface {
	// Next advances the iterator to the next node in the atomic trie and
	// returns true if there are more leaves to iterate
	Next() bool

	// Key returns the current database key that the iterator is iterating
	// returned []byte can be freely modified
	Key() []byte

	// Value returns the current database value that the iterator is iterating
	Value() []byte

	// BlockNumber returns the current block number
	BlockNumber() uint64

	// BlockchainID returns the current blockchain ID at the current block number
	BlockchainID() ids.ID

	// AtomicOps returns a map of blockchainIDs to the set of atomic requests
	// for that blockchainID at the current block number
	AtomicOps() *atomic.Requests

	// Error returns error, if any encountered during this iteration
	Error() error
}

// atomicTrie implements the AtomicTrie interface
type atomicTrie struct {
	commitInterval      uint64            // commit interval, same as commitHeightInterval by default
	metadataDB          database.Database // Underlying database containing the atomic trie metadata
	trieDB              *trie.Database    // Trie database
	lastCommittedRoot   common.Hash       // trie root of the most recent commit
	lastCommittedHeight uint64            // index height of the most recent commit
	lastAcceptedRoot    common.Hash       // most recent trie root passed to accept trie or the root of the atomic trie on intialization.
	codec               codec.Manager
	memoryCap           common.StorageSize
	tipBuffer           *core.BoundedBuffer[common.Hash]
}

// newAtomicTrie returns a new instance of a atomicTrie with a configurable commitHeightInterval, used in testing.
// Initializes the trie before returning it.
func newAtomicTrie(
	atomicTrieDB database.Database, metadataDB database.Database,
	codec codec.Manager, lastAcceptedHeight uint64, commitHeightInterval uint64,
) (*atomicTrie, error) {
	root, height, err := lastCommittedRootIfExists(metadataDB)
	if err != nil {
		return nil, err
	}
	// initialize to EmptyRootHash if there is no committed root.
	if root == (common.Hash{}) {
		root = types.EmptyRootHash
	}
	// If the last committed height is above the last accepted height, then we fall back to
	// the last commit below the last accepted height.
	if height > lastAcceptedHeight {
		height = nearestCommitHeight(lastAcceptedHeight, commitHeightInterval)
		root, err = getRoot(metadataDB, height)
		if err != nil {
			return nil, err
		}
	}

	trieDB := trie.NewDatabaseWithConfig(
		rawdb.NewDatabase(Database{atomicTrieDB}),
		&trie.Config{
			Cache: 64, // Allocate 64MB of memory for clean cache
		},
	)

	return &atomicTrie{
		commitInterval:      commitHeightInterval,
		metadataDB:          metadataDB,
		trieDB:              trieDB,
		codec:               codec,
		lastCommittedRoot:   root,
		lastCommittedHeight: height,
		tipBuffer:           core.NewBoundedBuffer(atomicTrieTipBufferSize, trieDB.Dereference),
		memoryCap:           atomicTrieMemoryCap,
		// Initialize lastAcceptedRoot to the last committed root.
		// If there were further blocks processed (ahead of the commit interval),
		// AtomicBackend will call InsertTrie/AcceptTrie on atomic ops
		// for those blocks.
		lastAcceptedRoot: root,
	}, nil
}

// lastCommittedRootIfExists returns the last committed trie root and height if it exists
// else returns empty common.Hash{} and 0
// returns error only if there are issues with the underlying data store
// or if values present in the database are not as expected
func lastCommittedRootIfExists(db database.Database) (common.Hash, uint64, error) {
	// read the last committed entry if it exists and set the root hash
	lastCommittedHeightBytes, err := db.Get(lastCommittedKey)
	switch {
	case err == database.ErrNotFound:
		return common.Hash{}, 0, nil
	case err != nil:
		return common.Hash{}, 0, err
	}

	height, err := database.ParseUInt64(lastCommittedHeightBytes)
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("expected value at lastCommittedKey to be a valid uint64: %w", err)
	}

	hash, err := db.Get(lastCommittedHeightBytes)
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("committed hash does not exist for committed height: %d: %w", height, err)
	}
	return common.BytesToHash(hash), height, nil
}

// nearestCommitheight returns the nearest multiple of commitInterval less than or equal to blockNumber
func nearestCommitHeight(blockNumber uint64, commitInterval uint64) uint64 {
	return blockNumber - (blockNumber % commitInterval)
}

func (a *atomicTrie) OpenTrie(root common.Hash) (*trie.Trie, error) {
	return trie.New(trie.TrieID(root), a.trieDB)
}

// commit calls commit on the underlying trieDB and updates metadata pointers.
func (a *atomicTrie) commit(height uint64, root common.Hash) error {
	if err := a.trieDB.Commit(root, false); err != nil {
		return err
	}
	log.Info("committed atomic trie", "root", root.String(), "height", height)
	return a.updateLastCommitted(root, height)
}

func (a *atomicTrie) UpdateTrie(trie *trie.Trie, height uint64, atomicOps map[ids.ID]*atomic.Requests) error {
	for blockchainID, requests := range atomicOps {
		valueBytes, err := a.codec.Marshal(codecVersion, requests)
		if err != nil {
			// highly unlikely but possible if atomic.Element
			// has a change that is unsupported by the codec
			return err
		}

		// key is [height]+[blockchainID]
		keyPacker := wrappers.Packer{Bytes: make([]byte, atomicKeyLength)}
		keyPacker.PackLong(height)
		keyPacker.PackFixedBytes(blockchainID[:])
		if err := trie.Update(keyPacker.Bytes, valueBytes); err != nil {
			return err
		}
	}

	return nil
}

// LastCommitted returns the last committed trie hash and last committed height
func (a *atomicTrie) LastCommitted() (common.Hash, uint64) {
	return a.lastCommittedRoot, a.lastCommittedHeight
}

// updateLastCommitted adds [height] -> [root] to the index and marks it as the last committed
// root/height pair.
func (a *atomicTrie) updateLastCommitted(root common.Hash, height uint64) error {
	heightBytes := database.PackUInt64(height)

	// now save the trie hash against the height it was committed at
	if err := a.metadataDB.Put(heightBytes, root[:]); err != nil {
		return err
	}

	// update lastCommittedKey with the current height
	if err := a.metadataDB.Put(lastCommittedKey, heightBytes); err != nil {
		return err
	}

	a.lastCommittedRoot = root
	a.lastCommittedHeight = height
	return nil
}

// Iterator returns a types.AtomicTrieIterator that iterates the trie from the given
// atomic trie root, starting at the specified [cursor].
func (a *atomicTrie) Iterator(root common.Hash, cursor []byte) (AtomicTrieIterator, error) {
	t, err := trie.New(trie.TrieID(root), a.trieDB)
	if err != nil {
		return nil, err
	}

	iter := trie.NewIterator(t.NodeIterator(cursor))
	return NewAtomicTrieIterator(iter, a.codec), iter.Err
}

func (a *atomicTrie) TrieDB() *trie.Database {
	return a.trieDB
}

// Root returns hash if it exists at specified height
// if trie was not committed at provided height, it returns
// common.Hash{} instead
func (a *atomicTrie) Root(height uint64) (common.Hash, error) {
	return getRoot(a.metadataDB, height)
}

// getRoot is a helper function to return the committed atomic trie root hash at [height]
// from [metadataDB].
func getRoot(metadataDB database.Database, height uint64) (common.Hash, error) {
	if height == 0 {
		// if root is queried at height == 0, return the empty root hash
		// this may occur if peers ask for the most recent state summary
		// and number of accepted blocks is less than the commit interval.
		return types.EmptyRootHash, nil
	}

	heightBytes := database.PackUInt64(height)
	hash, err := metadataDB.Get(heightBytes)
	switch {
	case err == database.ErrNotFound:
		return common.Hash{}, nil
	case err != nil:
		return common.Hash{}, err
	}
	return common.BytesToHash(hash), nil
}

func (a *atomicTrie) LastAcceptedRoot() common.Hash {
	return a.lastAcceptedRoot
}

func (a *atomicTrie) InsertTrie(nodes *trienode.NodeSet, root common.Hash) error {
	if nodes != nil {
		if err := a.trieDB.Update(root, types.EmptyRootHash, trienode.NewWithNodeSet(nodes)); err != nil {
			return err
		}
	}
	a.trieDB.Reference(root, common.Hash{})

	// The use of [Cap] in [insertTrie] prevents exceeding the configured memory
	// limit (and OOM) in case there is a large backlog of processing (unaccepted) blocks.
	if nodeSize, _ := a.trieDB.Size(); nodeSize <= a.memoryCap {
		return nil
	}
	if err := a.trieDB.Cap(a.memoryCap - ethdb.IdealBatchSize); err != nil {
		return fmt.Errorf("failed to cap atomic trie for root %s: %w", root, err)
	}

	return nil
}

// AcceptTrie commits the triedb at [root] if needed and returns true if a commit
// was performed.
func (a *atomicTrie) AcceptTrie(height uint64, root common.Hash) (bool, error) {
	hasCommitted := false
	// Because we do not accept the trie at every height, we may need to
	// populate roots at prior commit heights that were skipped.
	for nextCommitHeight := a.lastCommittedHeight + a.commitInterval; nextCommitHeight < height; nextCommitHeight += a.commitInterval {
		if err := a.commit(nextCommitHeight, a.lastAcceptedRoot); err != nil {
			return false, err
		}
		hasCommitted = true
	}

	// Attempt to dereference roots at least [tipBufferSize] old
	//
	// Note: It is safe to dereference roots that have been committed to disk
	// (they are no-ops).
	a.tipBuffer.Insert(root)

	// Commit this root if we have reached the [commitInterval].
	if height%a.commitInterval == 0 {
		if err := a.commit(height, root); err != nil {
			return false, err
		}
		hasCommitted = true
	}

	a.lastAcceptedRoot = root
	return hasCommitted, nil
}

func (a *atomicTrie) RejectTrie(root common.Hash) error {
	a.trieDB.Dereference(root)
	return nil
}
