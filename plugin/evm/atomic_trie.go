// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/statesync/types"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	commitHeightInterval = uint64(4096)
	progressLogUpdate    = 30 * time.Second
)

var (
	lastCommittedKey = []byte("atomicTrieLastCommittedBlock")
)

// atomicTrie implements the types.AtomicTrie interface
// using the eth trie.Trie implementation
type atomicTrie struct {
	commitHeightInterval uint64              // commit interval, same as commitHeightInterval by default
	db                   *versiondb.Database // Underlying database
	bonusBlocks          map[uint64]ids.ID   // Map of height to blockID for blocks to skip indexing
	metadataDB           database.Database   // Underlying database containing the atomic trie metadata
	atomicTrieDB         database.Database   // Underlying database containing the atomic trie
	trieDB               *trie.Database      // Trie database
	trie                 *trie.Trie          // Atomic trie.Trie mapping key (height+blockchainID) and value (codec serialized atomic.Requests)
	repo                 AtomicTxRepository
	lastCommittedHash    common.Hash // trie root hash of the most recent commit
	lastCommittedHeight  uint64      // index height of the most recent commit
	codec                codec.Manager
	log                  log.Logger // struct logger
}

var _ types.AtomicTrie = &atomicTrie{}

// NewAtomicTrie returns a new instance of a atomicTrie with the default commitHeightInterval.
// Initializes the trie before returning it.
func NewAtomicTrie(db *versiondb.Database, bonusBlocks map[uint64]ids.ID, repo AtomicTxRepository, codec codec.Manager, lastAcceptedHeight uint64) (types.AtomicTrie, error) {
	return newAtomicTrie(db, bonusBlocks, repo, codec, lastAcceptedHeight, commitHeightInterval)
}

// newAtomicTrie returns a new instance of a atomicTrie with a configurable commitHeightInterval, used in testing.
// Initializes the trie before returning it.
func newAtomicTrie(
	db *versiondb.Database, bonusBlocks map[uint64]ids.ID, repo AtomicTxRepository, codec codec.Manager, lastAcceptedHeight uint64, commitHeightInterval uint64,
) (*atomicTrie, error) {
	atomicTrieDB := prefixdb.New(atomicTrieDBPrefix, db)
	metadataDB := prefixdb.New(atomicTrieMetaDBPrefix, db)
	root, height, err := lastCommittedRootIfExists(metadataDB)
	if err != nil {
		return nil, err
	}

	triedb := trie.NewDatabaseWithConfig(
		Database{atomicTrieDB},
		&trie.Config{
			Cache:     10,    // Allocate 10MB of memory for clean cache
			Preimages: false, // Keys are not hashed, so there is no need for preimages
		},
	)
	t, err := trie.New(root, triedb)
	if err != nil {
		return nil, err
	}

	atomicTrie := &atomicTrie{
		commitHeightInterval: commitHeightInterval,
		db:                   db,
		bonusBlocks:          bonusBlocks,
		atomicTrieDB:         atomicTrieDB,
		metadataDB:           metadataDB,
		trieDB:               triedb,
		trie:                 t,
		repo:                 repo,
		codec:                codec,
		lastCommittedHash:    root,
		lastCommittedHeight:  height,
		log:                  log.New("c", "atomicTrie"),
	}
	return atomicTrie, atomicTrie.initialize(lastAcceptedHeight)
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
	case len(lastCommittedHeightBytes) != wrappers.LongLen:
		return common.Hash{}, 0, fmt.Errorf("expected value of lastCommittedKey to be %d but was %d", wrappers.LongLen, len(lastCommittedHeightBytes))
	}
	height := binary.BigEndian.Uint64(lastCommittedHeightBytes)
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

// initializes the atomic trie using the atomic repository height index.
// Iterating from the last indexed height to lastAcceptedBlockNumber, making a single commit at the
// most recent height divisible by the commitInterval.
// Subsequent updates to this trie are made using the Index call as blocks are accepted.
func (a *atomicTrie) initialize(lastAcceptedBlockNumber uint64) error {
	start := time.Now()
	a.log.Info("initializing atomic trie", "lastAcceptedBlockNumber", lastAcceptedBlockNumber)
	// commitHeight is the highest block that can be committed i.e. is divisible by b.commitHeightInterval
	// Txs from heights greater than commitHeight are to be included in the trie corresponding to the block at
	// commitHeight+b.commitHeightInterval, which has not been accepted yet.
	commitHeight := nearestCommitHeight(lastAcceptedBlockNumber, a.commitHeightInterval)
	uncommittedOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests, lastAcceptedBlockNumber-commitHeight)

	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, a.lastCommittedHeight)
	// iterate by height, from lastCommittedHeight to the lastAcceptedBlockNumber
	iter := a.repo.IterateByHeight(heightBytes)
	defer iter.Release()

	preCommitBlockIndexed := 0
	postCommitTxIndexed := 0
	lastUpdate := time.Now()

	// keep track of the latest generated trie's root and height.
	lastHash := common.Hash{}
	lastHeight := a.lastCommittedHeight
	for iter.Next() {
		// Get the height and transactions for this iteration (from the key and value, respectively)
		// iterate over the transactions, indexing them if the height is < commit height
		// otherwise, add the atomic operations from the transaction to the uncommittedOpsMap
		height := binary.BigEndian.Uint64(iter.Key())
		txs, err := ExtractAtomicTxs(iter.Value(), true, a.codec)
		if err != nil {
			return err
		}

		// combine atomic operations from all transactions at this block height
		combinedOps, err := mergeAtomicOps(txs)
		if err != nil {
			return err
		}

		if _, skipBonusBlock := a.bonusBlocks[height]; skipBonusBlock {
			// If [height] is a bonus block, do not index the atomic operations into the trie
		} else if height > commitHeight {
			// if height is greater than commit height, add it to the map so that we can write it later
			// this is to ensure we have all the data before the commit height so that we can commit the
			// trie
			uncommittedOpsMap[height] = combinedOps
		} else {
			if err := a.updateTrie(height, combinedOps); err != nil {
				return err
			}
			preCommitBlockIndexed++
		}

		if time.Since(lastUpdate) > progressLogUpdate {
			a.log.Info("imported entries into atomic trie pre-commit", "heightsIndexed", preCommitBlockIndexed)
			lastUpdate = time.Now()
		}

		// if height has reached or skipped over the next commit interval,
		// keep track of progress and keep commit size under commitSizeCap
		if lastHeight < nearestCommitHeight(height, a.commitHeightInterval) {
			hash, _, err := a.trie.Commit(nil)
			if err != nil {
				return err
			}
			// Dereference lashHash to avoid writing more intermediary
			// trie nodes than needed to disk, while keeping the commit
			// size under commitSizeCap (approximately).
			if (lastHash != common.Hash{}) {
				a.trieDB.Dereference(lastHash)
			}
			storage, _ := a.trieDB.Size()
			if storage > commitSizeCap {
				a.log.Info("committing atomic trie progress", "storage", storage)
				a.commit(height)
				// Flush any remaining changes that have not been committed yet in the versiondb.
				if err := a.db.Commit(); err != nil {
					return err
				}
			}
			lastHash = hash
			lastHeight = height
		}
	}
	if err := iter.Error(); err != nil {
		return err
	}

	// Note: we should never create a commit at the genesis block (should not contain any atomic txs)
	if lastAcceptedBlockNumber == 0 {
		return nil
	}
	// now that all heights < commitHeight have been processed
	// commit the trie
	if err := a.commit(commitHeight); err != nil {
		return err
	}
	// Flush any remaining changes that have not been committed yet in the versiondb.
	if err := a.db.Commit(); err != nil {
		return err
	}

	// process uncommitted ops for heights > commitHeight
	for height, ops := range uncommittedOpsMap {
		if err := a.updateTrie(height, ops); err != nil {
			return err
		}

		postCommitTxIndexed++
		if time.Since(lastUpdate) > progressLogUpdate {
			a.log.Info("imported entries into atomic trie post-commit", "entriesIndexed", postCommitTxIndexed)
			lastUpdate = time.Now()
		}
	}

	a.log.Info(
		"finished initializing atomic trie",
		"lastAcceptedBlockNumber", lastAcceptedBlockNumber,
		"preCommitEntriesIndexed", preCommitBlockIndexed, "postCommitEntriesIndexed", postCommitTxIndexed,
		"time", time.Since(start),
	)
	return nil
}

// Index updates the trie with entries in atomicOps
// This function updates the following:
// - heightBytes => trie root hash (if the trie was committed)
// - lastCommittedBlock => height (if the trie was committed)
func (a *atomicTrie) Index(height uint64, atomicOps map[ids.ID]*atomic.Requests) error {
	if err := a.validateIndexHeight(height); err != nil {
		return err
	}

	if err := a.updateTrie(height, atomicOps); err != nil {
		return err
	}

	if height%a.commitHeightInterval == 0 {
		return a.commit(height)
	}

	return nil
}

// validateIndexHeight returns an error if [height] is not currently valid to be indexed.
func (a *atomicTrie) validateIndexHeight(height uint64) error {
	// Do not allow a height that we have already passed to be indexed
	if height < a.lastCommittedHeight {
		return fmt.Errorf("height %d must be after last committed height %d", height, a.lastCommittedHeight)
	}

	// Do not allow a height that is more than a commit interval ahead
	// of the current index
	nextCommitHeight := a.lastCommittedHeight + a.commitHeightInterval
	if height > nextCommitHeight {
		return fmt.Errorf("height %d not within the next commit height %d", height, nextCommitHeight)
	}

	return nil
}

// commit calls commit on the trie to generate a root, commits the underlying trieDB, and updates the
// metadata pointers.
// assumes that the caller is aware of the commit rules i.e. the height being within commitInterval.
// returns the trie root from the commit
func (a *atomicTrie) commit(height uint64) error {
	hash, _, err := a.trie.Commit(nil)
	if err != nil {
		return err
	}

	a.log.Info("committed atomic trie", "hash", hash.String(), "height", height)
	if err := a.trieDB.Commit(hash, false, nil); err != nil {
		return err
	}

	// all good here, update the heightBytes
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	// now save the trie hash against the height it was committed at
	if err := a.metadataDB.Put(heightBytes, hash[:]); err != nil {
		return err
	}

	// update lastCommittedKey with the current height
	if err := a.metadataDB.Put(lastCommittedKey, heightBytes); err != nil {
		return err
	}

	a.lastCommittedHash = hash
	a.lastCommittedHeight = height
	return nil
}

func (a *atomicTrie) updateTrie(height uint64, atomicOps map[ids.ID]*atomic.Requests) error {
	for blockchainID, requests := range atomicOps {
		valueBytes, err := a.codec.Marshal(codecVersion, requests)
		if err != nil {
			// highly unlikely but possible if atomic.Element
			// has a change that is unsupported by the codec
			return err
		}

		// key is [height]+[blockchainID]
		keyPacker := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen+common.HashLength)}
		keyPacker.PackLong(height)
		keyPacker.PackFixedBytes(blockchainID[:])
		if err := a.trie.TryUpdate(keyPacker.Bytes, valueBytes); err != nil {
			return err
		}
	}

	return nil
}

// LastCommitted returns the last committed trie hash and last committed height
func (a *atomicTrie) LastCommitted() (common.Hash, uint64) {
	return a.lastCommittedHash, a.lastCommittedHeight
}

// Iterator returns a types.AtomicTrieIterator that iterates the trie from the given
// atomic trie root, starting at the specified height
func (a *atomicTrie) Iterator(root common.Hash, startHeight uint64) (types.AtomicTrieIterator, error) {
	startKey := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(startKey, startHeight)

	t, err := trie.New(root, a.trieDB)
	if err != nil {
		return nil, err
	}

	iter := trie.NewIterator(t.NodeIterator(startKey))
	return NewAtomicTrieIterator(iter, a.codec), iter.Err
}

func (a *atomicTrie) TrieDB() *trie.Database {
	return a.trieDB
}

// Root returns hash if it exists at specified height
// if trie was not committed at provided height, it returns
// common.Hash{} instead
func (a *atomicTrie) Root(height uint64) (common.Hash, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	hash, err := a.metadataDB.Get(heightBytes)
	switch {
	case err == database.ErrNotFound:
		return common.Hash{}, nil
	case err != nil:
		return common.Hash{}, err
	}
	return common.BytesToHash(hash), nil
}
