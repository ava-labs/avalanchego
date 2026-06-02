// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/evm/triedb/hashdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/evm/database"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	avalanchedatabase "github.com/ava-labs/avalanchego/database"
)

const (
	TrieKeyLength = wrappers.LongLen + common.HashLength

	atomicTrieMemoryCap = 64 * units.MiB
)

var lastCommittedKey = []byte("atomicTrieLastCommittedBlock")

// AtomicTrie is a trie that is used to store atomic operations.
type AtomicTrie struct {
	metadataDB          avalanchedatabase.Database // Underlying database containing the atomic trie metadata
	trieDB              *triedb.Database           // Trie database
	lastCommittedRoot   common.Hash                // trie root of the most recent commit
	lastCommittedHeight uint64                     // index height of the most recent commit
	lastAcceptedRoot    common.Hash                // most recent trie root passed to accept trie or the root of the atomic trie on intialization.
	codec               codec.Manager
	memoryCap           common.StorageSize
}

// newAtomicTrie returns a new instance of an AtomicTrie, initialized before
// returning it.
func newAtomicTrie(
	atomicTrieStorage avalanchedatabase.Database, metadataDB avalanchedatabase.Database,
	codec codec.Manager, lastAcceptedHeight uint64,
) (*AtomicTrie, error) {
	root, height, err := lastCommittedRootIfExists(metadataDB)
	if err != nil {
		return nil, err
	}
	// initialize to EmptyRootHash if there is no committed root.
	if root == (common.Hash{}) {
		root = types.EmptyRootHash
	}
	// If the last committed height is above the last accepted height, then we fall
	// back to the root committed at the last accepted height.
	if height > lastAcceptedHeight {
		height = lastAcceptedHeight
		root, err = getRoot(metadataDB, height)
		if err != nil {
			return nil, err
		}
	}

	trieDB := triedb.NewDatabase(
		rawdb.NewDatabase(database.New(atomicTrieStorage)),
		&triedb.Config{
			DBOverride: hashdb.Config{
				CleanCacheSize: 64 * units.MiB, // Allocate 64MB of memory for clean cache
			}.BackendConstructor,
		},
	)

	return &AtomicTrie{
		metadataDB:          metadataDB,
		trieDB:              trieDB,
		codec:               codec,
		lastCommittedRoot:   root,
		lastCommittedHeight: height,
		memoryCap:           atomicTrieMemoryCap,
		// Initialize lastAcceptedRoot to the last committed root.
		// If there were further blocks processed (not yet committed),
		// AtomicBackend will call InsertTrie/AcceptTrie on atomic ops
		// for those blocks.
		lastAcceptedRoot: root,
	}, nil
}

// lastCommittedRootIfExists returns the last committed trie root and height if it exists
// else returns empty common.Hash{} and 0
// returns error only if there are issues with the underlying data store
// or if values present in the database are not as expected
func lastCommittedRootIfExists(db avalanchedatabase.Database) (common.Hash, uint64, error) {
	// read the last committed entry if it exists and set the root hash
	lastCommittedHeightBytes, err := db.Get(lastCommittedKey)
	switch {
	case err == avalanchedatabase.ErrNotFound:
		return common.Hash{}, 0, nil
	case err != nil:
		return common.Hash{}, 0, err
	}

	height, err := avalanchedatabase.ParseUInt64(lastCommittedHeightBytes)
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("expected value at lastCommittedKey to be a valid uint64: %w", err)
	}

	hash, err := db.Get(lastCommittedHeightBytes)
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("committed hash does not exist for committed height: %d: %w", height, err)
	}
	return common.BytesToHash(hash), height, nil
}

func (a *AtomicTrie) OpenTrie(root common.Hash) (*trie.Trie, error) {
	return trie.New(trie.TrieID(root), a.trieDB)
}

// Commit calls Commit on the underlying trieDB and updates metadata pointers.
func (a *AtomicTrie) Commit(height uint64, root common.Hash) error {
	if err := a.trieDB.Commit(root, false); err != nil {
		return err
	}
	log.Debug("committed atomic trie", "root", root.String(), "height", height)
	return a.updateLastCommitted(root, height)
}

func (a *AtomicTrie) UpdateTrie(trie *trie.Trie, height uint64, atomicOps map[ids.ID]*avalancheatomic.Requests) error {
	for blockchainID, requests := range atomicOps {
		valueBytes, err := a.codec.Marshal(atomic.CodecVersion, requests)
		if err != nil {
			// highly unlikely but possible if atomic.Element
			// has a change that is unsupported by the codec
			return err
		}

		// key is [height]+[blockchainID]
		keyPacker := wrappers.Packer{Bytes: make([]byte, TrieKeyLength)}
		keyPacker.PackLong(height)
		keyPacker.PackFixedBytes(blockchainID[:])
		if err := trie.Update(keyPacker.Bytes, valueBytes); err != nil {
			return err
		}
	}

	return nil
}

// LastCommitted returns the last committed trie hash and last committed height
func (a *AtomicTrie) LastCommitted() (common.Hash, uint64) {
	return a.lastCommittedRoot, a.lastCommittedHeight
}

// updateLastCommitted adds [height] -> [root] to the index and marks it as the last committed
// root/height pair.
func (a *AtomicTrie) updateLastCommitted(root common.Hash, height uint64) error {
	heightBytes := avalanchedatabase.PackUInt64(height)

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

// Iterator returns an [atomicTrieIterator] that iterates the trie from the given
// atomic trie root, starting at the specified [cursor].
func (a *AtomicTrie) Iterator(root common.Hash, cursor []byte) (*atomicTrieIterator, error) {
	t, err := trie.New(trie.TrieID(root), a.trieDB)
	if err != nil {
		return nil, err
	}

	nodeIt, err := t.NodeIterator(cursor)
	if err != nil {
		return nil, err
	}
	iter := trie.NewIterator(nodeIt)
	return NewAtomicTrieIterator(iter, a.codec), iter.Err
}

func (a *AtomicTrie) TrieDB() *triedb.Database {
	return a.trieDB
}

// Root returns hash if it exists at specified height
// if trie was not committed at provided height, it returns
// common.Hash{} instead
func (a *AtomicTrie) Root(height uint64) (common.Hash, error) {
	return getRoot(a.metadataDB, height)
}

// getRoot is a helper function to return the committed atomic trie root hash at [height]
// from [metadataDB].
func getRoot(metadataDB avalanchedatabase.Database, height uint64) (common.Hash, error) {
	if height == 0 {
		// if root is queried at height == 0, return the empty root hash
		// this may occur if peers ask for the most recent state summary
		// and number of accepted blocks is less than the commit interval.
		return types.EmptyRootHash, nil
	}

	heightBytes := avalanchedatabase.PackUInt64(height)
	hash, err := metadataDB.Get(heightBytes)
	switch {
	case err == avalanchedatabase.ErrNotFound:
		return common.Hash{}, nil
	case err != nil:
		return common.Hash{}, err
	}
	return common.BytesToHash(hash), nil
}

func (a *AtomicTrie) LastAcceptedRoot() common.Hash {
	return a.lastAcceptedRoot
}

func (a *AtomicTrie) InsertTrie(nodes *trienode.NodeSet, root common.Hash) error {
	if nodes != nil {
		if err := a.trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
			return err
		}
	}
	if err := a.trieDB.Reference(root, common.Hash{}); err != nil {
		return err
	}

	// The use of [Cap] in [insertTrie] prevents exceeding the configured memory
	// limit (and OOM) in case there is a large backlog of processing (unaccepted) blocks.
	if _, nodeSize, _ := a.trieDB.Size(); nodeSize <= a.memoryCap {
		return nil
	}
	if err := a.trieDB.Cap(a.memoryCap - ethdb.IdealBatchSize); err != nil {
		return fmt.Errorf("failed to cap atomic trie for root %s: %w", root, err)
	}

	return nil
}

// AcceptTrie commits the triedb at root for height.
func (a *AtomicTrie) AcceptTrie(height uint64, root common.Hash) error {
	// Populate roots at any heights that were skipped since the last commit.
	for skippedHeight := a.lastCommittedHeight + 1; skippedHeight < height; skippedHeight++ {
		if err := a.Commit(skippedHeight, a.lastAcceptedRoot); err != nil {
			return err
		}
	}

	if err := a.Commit(height, root); err != nil {
		return err
	}

	// The following dereferences, if any, the previously inserted root.
	// This one can be dereferenced whether it has been:
	// - committed, in which case the dereference is a no-op
	// - not committted, in which case the current root we are inserting contains
	//   references to all the relevant data from the previous root, so the previous
	//   root can be dereferenced.
	if err := a.trieDB.Dereference(a.lastAcceptedRoot); err != nil {
		return err
	}
	a.lastAcceptedRoot = root
	return nil
}

func (a *AtomicTrie) RejectTrie(root common.Hash) error {
	return a.trieDB.Dereference(root)
}
