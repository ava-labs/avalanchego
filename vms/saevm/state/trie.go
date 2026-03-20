// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/evm/triedb/hashdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/triedb"
)

var (
	trieMetadataPrefix     = prefixdb.MakePrefix([]byte("atomicTrieMetaDB"))
	lastCommittedHeightKey = prefixdb.PrefixKey(trieMetadataPrefix, []byte("atomicTrieLastCommittedBlock"))
	lastAppliedHeightKey   = prefixdb.PrefixKey(trieMetadataPrefix, []byte("atomicTrieLastAppliedBlock"))

	trieStoragePrefix = []byte("atomicTrieDB")
)

func trieMetadataHeightKey(height uint64) []byte {
	return prefixdb.PrefixKey(trieMetadataPrefix, database.PackUInt64(height))
}

// ReadLastCommittedRoot returns the last committed trie root and height. If
// none have been written, it returns an empty trie root for the genesis block.
func ReadLastCommittedRoot(db database.KeyValueReader) (common.Hash, uint64, error) {
	height, err := database.GetUInt64(db, lastCommittedHeightKey)
	switch {
	case err == database.ErrNotFound:
		return types.EmptyRootHash, 0, nil
	case err != nil:
		return common.Hash{}, 0, fmt.Errorf("failed to get last committed height: %w", err)
	}

	root, err := ReadCommittedRoot(db, height)
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("failed to get committed root for height %d: %w", height, err)
	}
	return root, height, nil
}

// ReadCommittedRoot gets the committed trie root hash at height from the
// database.
func ReadCommittedRoot(db database.KeyValueReader, height uint64) (common.Hash, error) {
	if height == 0 {
		// if root is queried at height == 0, return the empty root hash
		// this may occur if peers ask for the most recent state summary
		// and number of accepted blocks is less than the commit interval.
		return types.EmptyRootHash, nil
	}

	root, err := database.GetID(db, trieMetadataHeightKey(height))
	if err != nil {
		return common.Hash{}, err
	}
	return common.Hash(root), nil
}

// WriteCommittedRoot maps height to root and marks it as the last committed
// entry.
func WriteCommittedRoot(db database.KeyValueWriter, height uint64, root common.Hash) error {
	if err := database.PutUInt64(db, lastCommittedHeightKey, height); err != nil {
		return err
	}
	return database.PutID(db, trieMetadataHeightKey(height), ids.ID(root))
}

// ReadLastAppliedHeight returns the last applied height. If the genesis block.
func ReadLastAppliedHeight(db database.KeyValueReader) (uint64, error) {
	height, err := database.GetUInt64(db, lastAppliedHeightKey)
	switch {
	case err == database.ErrNotFound:
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("failed to get last applied height: %w", err)
	default:
		return height, nil
	}
}

// WriteLastAppliedHeight stores the last applied height.
func WriteLastAppliedHeight(db database.KeyValueWriter, height uint64) error {
	return database.PutUInt64(db, lastAppliedHeightKey, height)
}

func NewTrieDB(avaDB database.Database) *triedb.Database {
	return triedb.NewDatabase(
		rawdb.NewDatabase(evmdb.New(prefixdb.NewNested(trieStoragePrefix, avaDB))),
		&triedb.Config{
			DBOverride: hashdb.Config{
				CleanCacheSize: 64 * units.MiB, // Allocate 64MB of memory for clean cache
			}.BackendConstructor,
		},
	)
}
