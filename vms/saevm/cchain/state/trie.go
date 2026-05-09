// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// This file mirrors graft/coreth/plugin/evm/atomic/state/atomic_trie.go.
// References to the old code are temporary; they will be removed once that
// package is deleted.

package state

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/evm/triedb/hashdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

// Database layout (must remain compatible with the old coreth code so that
// migrations don't require a rewrite of these prefixes):
//
//	atomicTrieMetaDB / "atomicTrieLastCommittedBlock" -> uint64(8 BE)
//	atomicTrieMetaDB / [height(8 BE)]                 -> trie root (32 bytes)
//	atomicTrieMetaDB / "atomicTrieLastAppliedBlock"   -> uint64(8 BE) [new, not in old]
//	atomicTrieDB     / <trie node keys>               -> RLP nodes (libevm trie)
var (
	trieMetadataPrefix     = prefixdb.MakePrefix([]byte("atomicTrieMetaDB"))
	lastCommittedHeightKey = prefixdb.PrefixKey(trieMetadataPrefix, []byte("atomicTrieLastCommittedBlock"))
	lastAppliedHeightKey   = prefixdb.PrefixKey(trieMetadataPrefix, []byte("atomicTrieLastAppliedBlock"))

	trieStoragePrefix = []byte("atomicTrieDB")
)

// trieKeyLength matches AtomicTrie.TrieKeyLength in the old code: each leaf
// is keyed by [height(8 BE) || blockchainID(32)].
const trieKeyLength = wrappers.LongLen + ids.IDLen

// Memory cap for the in-memory trieDB between commits. Matches old coreth's
// atomicTrieMemoryCap (atomic_trie.go:33).
const (
	trieMemoryCap     = 64 * units.MiB
	trieMemoryCapTrim = trieMemoryCap - ethdb.IdealBatchSize
)

func trieMetadataHeightKey(height uint64) []byte {
	return prefixdb.PrefixKey(trieMetadataPrefix, database.PackUInt64(height))
}

// readLastCommittedRoot returns the last committed trie root and height. If
// none have been written, it returns the empty root for the genesis block.
func readLastCommittedRoot(db database.KeyValueReader) (common.Hash, uint64, error) {
	height, err := database.GetUInt64(db, lastCommittedHeightKey)
	switch {
	case err == database.ErrNotFound:
		return types.EmptyRootHash, 0, nil
	case err != nil:
		return common.Hash{}, 0, fmt.Errorf("getting last committed height: %w", err)
	}

	root, err := readCommittedRoot(db, height)
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("getting committed root for height %d: %w", height, err)
	}
	return root, height, nil
}

// readCommittedRoot returns the trie root committed at height, or the empty
// root if height == 0.
func readCommittedRoot(db database.KeyValueReader, height uint64) (common.Hash, error) {
	if height == 0 {
		return types.EmptyRootHash, nil
	}

	root, err := database.GetID(db, trieMetadataHeightKey(height))
	if err != nil {
		return common.Hash{}, err
	}
	return common.Hash(root), nil
}

// writeCommittedRoot maps height to root and marks it as the last committed
// entry.
func writeCommittedRoot(db database.KeyValueWriter, height uint64, root common.Hash) error {
	if err := database.PutUInt64(db, lastCommittedHeightKey, height); err != nil {
		return fmt.Errorf("writing last committed height: %w", err)
	}
	if err := database.PutID(db, trieMetadataHeightKey(height), ids.ID(root)); err != nil {
		return fmt.Errorf("writing committed root for height %d: %w", height, err)
	}
	return nil
}

// readLastAppliedHeight returns the last height applied to shared memory, or
// 0 if no apply has been recorded.
func readLastAppliedHeight(db database.KeyValueReader) (uint64, error) {
	height, err := database.GetUInt64(db, lastAppliedHeightKey)
	switch {
	case err == database.ErrNotFound:
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("getting last applied height: %w", err)
	default:
		return height, nil
	}
}

// writeLastAppliedHeight records the last height applied to shared memory.
func writeLastAppliedHeight(db database.KeyValueWriter, height uint64) error {
	if err := database.PutUInt64(db, lastAppliedHeightKey, height); err != nil {
		return fmt.Errorf("writing last applied height: %w", err)
	}
	return nil
}

func newTrieDB(avaDB database.Database) *triedb.Database {
	return triedb.NewDatabase(
		rawdb.NewDatabase(evmdb.New(prefixdb.NewNested(trieStoragePrefix, avaDB))),
		&triedb.Config{
			DBOverride: hashdb.Config{
				CleanCacheSize: 64 * units.MiB,
			}.BackendConstructor,
		},
	)
}

// nearestCommitHeight returns the largest multiple of commitInterval that is
// less than or equal to height. Mirrors atomic_trie.go:126.
func nearestCommitHeight(height, commitInterval uint64) uint64 {
	return height - (height % commitInterval)
}

// trieState owns the in-memory atomic trie. It tracks the in-memory tip
// (lastAcceptedRoot) and the height of the most recent on-disk commit.
//
// Mirrors AtomicTrie in graft/coreth/plugin/evm/atomic/state/atomic_trie.go.
type trieState struct {
	trieDB              *triedb.Database
	commitInterval      uint64
	lastAcceptedRoot    common.Hash
	lastCommittedHeight uint64
}

// newTrieState initializes the in-memory trie state from db. If the on-disk
// last-committed height is above lastAcceptedHeight (which can happen if
// state was pruned back), it falls back to the nearest commit at or below
// lastAcceptedHeight.
//
// Mirrors newAtomicTrie in atomic_trie.go:52.
func newTrieState(db database.Database, lastAcceptedHeight, commitInterval uint64) (*trieState, error) {
	root, height, err := readLastCommittedRoot(db)
	if err != nil {
		return nil, err
	}

	if height > lastAcceptedHeight {
		height = nearestCommitHeight(lastAcceptedHeight, commitInterval)
		root, err = readCommittedRoot(db, height)
		if err != nil {
			return nil, fmt.Errorf("loading committed root at fallback height %d: %w", height, err)
		}
	}

	return &trieState{
		trieDB:              newTrieDB(db),
		commitInterval:      commitInterval,
		lastAcceptedRoot:    root,
		lastCommittedHeight: height,
	}, nil
}

// apply writes ops into the atomic trie at height. The committed-root
// metadata write (when height crosses a commit boundary) is added to batch;
// trie node writes are flushed by the underlying trieDB. Returns the new
// trie root.
//
// Mirrors AtomicBackend.InsertTxs (atomic_backend.go:345) followed by
// AtomicTrie.AcceptTrie (atomic_trie.go:265).
func (t *trieState) apply(
	batch database.KeyValueWriter,
	height uint64,
	ops map[ids.ID]*chainsatomic.Requests,
) (common.Hash, error) {
	tr, err := trie.New(trie.TrieID(t.lastAcceptedRoot), t.trieDB)
	if err != nil {
		return common.Hash{}, fmt.Errorf("opening atomic trie at root %s: %w", t.lastAcceptedRoot, err)
	}

	for chainID, requests := range ops {
		valueBytes, err := marshalAtomicRequests(requests)
		if err != nil {
			return common.Hash{}, fmt.Errorf("marshaling atomic requests for chain %s: %w", chainID, err)
		}

		keyPacker := wrappers.Packer{Bytes: make([]byte, trieKeyLength)}
		keyPacker.PackLong(height)
		keyPacker.PackFixedBytes(chainID[:])
		if err := tr.Update(keyPacker.Bytes, valueBytes); err != nil {
			return common.Hash{}, fmt.Errorf("inserting trie key for chain %s: %w", chainID, err)
		}
	}

	root, nodes, err := tr.Commit(false)
	if err != nil {
		return common.Hash{}, fmt.Errorf("committing in-memory trie at height %d: %w", height, err)
	}
	if nodes != nil {
		if err := t.trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
			return common.Hash{}, fmt.Errorf("updating trieDB with new nodes: %w", err)
		}
	}
	if err := t.trieDB.Reference(root, common.Hash{}); err != nil {
		return common.Hash{}, fmt.Errorf("referencing root %s: %w", root, err)
	}

	// Bound in-memory trieDB size to prevent OOM if many heights pass between
	// disk commits. Mirrors AtomicTrie.InsertTrie (atomic_trie.go:251).
	if _, nodeSize, _ := t.trieDB.Size(); nodeSize > trieMemoryCap {
		if err := t.trieDB.Cap(trieMemoryCapTrim); err != nil {
			return common.Hash{}, fmt.Errorf("capping atomic trie at root %s: %w", root, err)
		}
	}

	// Catch up missed commit boundaries using the previous tip
	// (t.lastAcceptedRoot still holds the prior root here).
	// Mirrors the for-loop in AtomicTrie.AcceptTrie (atomic_trie.go:269).
	for nextCommitHeight := t.lastCommittedHeight + t.commitInterval; nextCommitHeight < height; nextCommitHeight += t.commitInterval {
		if err := t.commit(batch, nextCommitHeight, t.lastAcceptedRoot); err != nil {
			return common.Hash{}, err
		}
	}

	if height%t.commitInterval == 0 {
		if err := t.commit(batch, height, root); err != nil {
			return common.Hash{}, err
		}
	}

	// Dereference the previous tip. Either it was already committed (no-op)
	// or the new root references all live data from it.
	if err := t.trieDB.Dereference(t.lastAcceptedRoot); err != nil {
		return common.Hash{}, fmt.Errorf("dereferencing prior root %s: %w", t.lastAcceptedRoot, err)
	}
	t.lastAcceptedRoot = root
	return root, nil
}

// commit flushes the trieDB at root and records (height, root) in batch.
// Mirrors AtomicTrie.Commit (atomic_trie.go:135).
func (t *trieState) commit(batch database.KeyValueWriter, height uint64, root common.Hash) error {
	if err := t.trieDB.Commit(root, false); err != nil {
		return fmt.Errorf("committing trieDB at height %d root %s: %w", height, root, err)
	}
	if err := writeCommittedRoot(batch, height, root); err != nil {
		return err
	}
	t.lastCommittedHeight = height
	return nil
}
