// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

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

	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/evm/triedb/hashdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

// Database layout — these prefixes must remain byte-compatible with the
// indices written by [state.AtomicTrie] so the chain can read entries
// produced by older nodes:
//
//	atomicTrieMetaDB / "atomicTrieLastCommittedBlock" -> uint64(8 BE)
//	atomicTrieMetaDB / [height(8 BE)]                 -> trie root (32 bytes)
//	atomicTrieMetaDB / "atomicTrieLastAppliedBlock"   -> uint64(8 BE)
//	atomicTrieDB     / <trie node keys>               -> RLP nodes (libevm trie)
var (
	trieMetadataPrefix     = prefixdb.MakePrefix([]byte("atomicTrieMetaDB"))
	lastCommittedHeightKey = prefixdb.PrefixKey(trieMetadataPrefix, []byte("atomicTrieLastCommittedBlock"))
	lastAppliedHeightKey   = prefixdb.PrefixKey(trieMetadataPrefix, []byte("atomicTrieLastAppliedBlock"))

	trieStoragePrefix = []byte("atomicTrieDB")
)

// trieKeyLength is the length of an atomic trie leaf key, which is laid
// out as [height(8 BE) || blockchainID(32)].
//
// Matches [state.TrieKeyLength].
const trieKeyLength = wrappers.LongLen + ids.IDLen

// Memory cap for the in-memory trieDB between commits.
//
// Matches [state.AtomicTrie.memoryCap].
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

// applyTrie merges the atomic ops in sorted and writes them into the trie
// at height. The committed-root metadata write (when height crosses a
// commit boundary) is added to batch; trie node writes are flushed by the
// underlying trieDB. sorted must be sorted by tx ID.
//
// It merges [state.AtomicBackend.InsertTxs] and [state.AtomicTrie.AcceptTrie].
func (s *State) applyTrie(batch database.KeyValueWriter, height uint64, sorted []*tx.Tx) error {
	ops, err := mergeAtomicOps(sorted)
	if err != nil {
		return err
	}

	tr, err := trie.New(trie.TrieID(s.lastAcceptedRoot), s.trieDB)
	if err != nil {
		return fmt.Errorf("opening atomic trie at root %s: %w", s.lastAcceptedRoot, err)
	}

	for chainID, requests := range ops {
		valueBytes, err := c.Marshal(codecVersion, requests)
		if err != nil {
			return fmt.Errorf("marshaling atomic requests for chain %s: %w", chainID, err)
		}

		keyPacker := wrappers.Packer{Bytes: make([]byte, trieKeyLength)}
		keyPacker.PackLong(height)
		keyPacker.PackFixedBytes(chainID[:])
		if err := tr.Update(keyPacker.Bytes, valueBytes); err != nil {
			return fmt.Errorf("inserting trie key for chain %s: %w", chainID, err)
		}
	}

	root, nodes, err := tr.Commit(false)
	if err != nil {
		return fmt.Errorf("committing in-memory trie at height %d: %w", height, err)
	}
	if nodes != nil {
		if err := s.trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
			return fmt.Errorf("updating trieDB with new nodes: %w", err)
		}
	}
	if err := s.trieDB.Reference(root, common.Hash{}); err != nil {
		return fmt.Errorf("referencing root %s: %w", root, err)
	}

	// Bound in-memory trieDB size to prevent OOM if many heights pass between
	// disk commits. Mirrors [state.AtomicTrie.InsertTrie].
	if _, nodeSize, _ := s.trieDB.Size(); nodeSize > trieMemoryCap {
		if err := s.trieDB.Cap(trieMemoryCapTrim); err != nil {
			return fmt.Errorf("capping atomic trie at root %s: %w", root, err)
		}
	}

	// Catch up missed commit boundaries using the previous tip
	// (s.lastAcceptedRoot still holds the prior root here). Mirrors the
	// for-loop in [state.AtomicTrie.AcceptTrie].
	for nextCommitHeight := s.lastCommittedHeight + commitInterval; nextCommitHeight < height; nextCommitHeight += commitInterval {
		if err := s.commitTrie(batch, nextCommitHeight, s.lastAcceptedRoot); err != nil {
			return err
		}
	}

	if height%commitInterval == 0 {
		if err := s.commitTrie(batch, height, root); err != nil {
			return err
		}
	}

	// Dereference the previous tip. Either it was already committed (no-op)
	// or the new root references all live data from it.
	if err := s.trieDB.Dereference(s.lastAcceptedRoot); err != nil {
		return fmt.Errorf("dereferencing prior root %s: %w", s.lastAcceptedRoot, err)
	}
	s.lastAcceptedRoot = root
	return nil
}

// Mirrors [state.AtomicTrie.Commit].

// commitTrie flushes the trieDB at root and records (height, root) in batch.
func (s *State) commitTrie(batch database.KeyValueWriter, height uint64, root common.Hash) error {
	if err := s.trieDB.Commit(root, false); err != nil {
		return fmt.Errorf("committing trieDB at height %d root %s: %w", height, root, err)
	}
	if err := writeCommittedRoot(batch, height, root); err != nil {
		return err
	}
	s.lastCommittedHeight = height
	return nil
}
