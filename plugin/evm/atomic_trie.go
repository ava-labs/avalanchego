// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	commitHeightInterval = uint64(4096)
	progressLogUpdate    = 30 * time.Second
)

var (
	lastCommittedKey             = []byte("atomicTrieLastCommittedBlock")
	lastAppliedToSharedMemoryKey = []byte("atomicTrieLastAppliedToSharedMemory")
)

// AtomicTrie maintains an index of atomic operations by blockchainIDs for every block
// height containing atomic transactions. The backing data structure for this index is
// a Trie. The keys of the trie are block heights and the values (leaf nodes)
// are the atomic operations applied to shared memory while processing the block accepted
// at the corresponding height.
type AtomicTrie interface {
	// Index indexes the given atomicOps at the specified block height
	// Returns an optional root hash
	// A non-empty root hash is returned when the atomic trie has been committed
	// Atomic trie is committed if the block height is multiple of commit interval
	Index(height uint64, atomicOps map[ids.ID]*atomic.Requests) error

	// Iterator returns an AtomicTrieIterator to iterate the trie at the given
	// root hash
	Iterator(hash common.Hash, startHeight uint64) (AtomicTrieIterator, error)

	// LastCommitted returns the last committed hash and corresponding block height
	LastCommitted() (common.Hash, uint64)

	// UpdateLastCommitted sets the state to last committed hash and height
	UpdateLastCommitted(hash common.Hash, height uint64) error

	// TrieDB returns the underlying trie database
	TrieDB() *trie.Database

	// Root returns hash if it exists at specified height
	// if trie was not committed at provided height, it returns
	// common.Hash{} instead
	Root(height uint64) (common.Hash, error)

	// ApplyToSharedMemory iterates over atomic ops indexed in the trie and applies them
	// to sharedMemory, for heights less than or equal to [lastAcceptedBlock].
	ApplyToSharedMemory(lastAcceptedBlock uint64) error

	// SetLastAppliedToSharedMemoryHeight marks the atomic trie as containing atomic ops
	// that must be applied to shared memory (on initialization). Heights less than or
	// equal to [height] must already be applied to shared memory.
	SetLastAppliedToSharedMemoryHeight(height uint64) error
}

// AtomicTrieIterator is a stateful iterator that iterates the leafs of an AtomicTrie
type AtomicTrieIterator interface {
	// Next advances the iterator to the next node in the atomic trie and
	// returns true if there are more nodes to iterate
	Next() bool

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
	sharedMemory         atomic.SharedMemory
}

var _ AtomicTrie = &atomicTrie{}

// NewAtomicTrie returns a new instance of a atomicTrie with the default commitHeightInterval.
// Initializes the trie before returning it.
func NewAtomicTrie(
	db *versiondb.Database, sharedMemory atomic.SharedMemory,
	bonusBlocks map[uint64]ids.ID, repo AtomicTxRepository, codec codec.Manager, lastAcceptedHeight uint64,
) (AtomicTrie, error) {
	return newAtomicTrie(db, sharedMemory, bonusBlocks, repo, codec, lastAcceptedHeight, commitHeightInterval)
}

// newAtomicTrie returns a new instance of a atomicTrie with a configurable commitHeightInterval, used in testing.
// Initializes the trie before returning it.
func newAtomicTrie(
	db *versiondb.Database, sharedMemory atomic.SharedMemory,
	bonusBlocks map[uint64]ids.ID, repo AtomicTxRepository, codec codec.Manager,
	lastAcceptedHeight uint64, commitHeightInterval uint64,
) (*atomicTrie, error) {
	if sharedMemory == nil {
		return nil, errors.New("non-nil sharedMemory required in newAtomicTrie")
	}
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
		sharedMemory:         sharedMemory,
	}
	// ApplyToSharedMemory is called here in case the node was shut down after state sync
	// in the middle of applying atomic ops to shared memory, to finish that operation.
	// In normal operations, the atomicTrie will not contain operations for heights greater
	// than lastAcceptedHeight and this call will be a noop.
	if err := atomicTrie.ApplyToSharedMemory(lastAcceptedHeight); err != nil {
		return nil, err
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
	// finalCommitHeight is the highest block that can be committed i.e. is divisible by b.commitHeightInterval
	// Txs from heights greater than commitHeight are to be included in the trie corresponding to the block at
	// finalCommitHeight+b.commitHeightInterval, which has not been accepted yet.
	finalCommitHeight := nearestCommitHeight(lastAcceptedBlockNumber, a.commitHeightInterval)
	uncommittedOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests, lastAcceptedBlockNumber-finalCommitHeight)

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
		} else if height > finalCommitHeight {
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
		commitHeight := nearestCommitHeight(height, a.commitHeightInterval)
		if lastHeight < commitHeight {
			hash, _, err := a.trie.Commit(nil)
			if err != nil {
				return err
			}
			// Dereference lashHash to avoid writing more intermediary
			// trie nodes than needed to disk, while keeping the commit
			// size under commitSizeCap (approximately).
			// Check [lastHash != hash] here to avoid dereferencing the
			// trie root in case there were no atomic txs since the
			// last commit.
			if (lastHash != common.Hash{} && lastHash != hash) {
				a.trieDB.Dereference(lastHash)
			}
			storage, _ := a.trieDB.Size()
			if storage > commitSizeCap {
				a.log.Info("committing atomic trie progress", "storage", storage)
				a.commit(commitHeight)
				// Flush any remaining changes that have not been committed yet in the versiondb.
				if err := a.db.Commit(); err != nil {
					return err
				}
			}
			lastHash = hash
			lastHeight = commitHeight
		}
	}
	if err := iter.Error(); err != nil {
		return err
	}

	// Note: we should never create a commit at the genesis block (should not contain any atomic txs)
	if lastAcceptedBlockNumber == 0 {
		return nil
	}
	// now that all heights < finalCommitHeight have been processed
	// commit the trie
	if err := a.commit(finalCommitHeight); err != nil {
		return err
	}
	// Flush any remaining changes that have not been committed yet in the versiondb.
	if err := a.db.Commit(); err != nil {
		return err
	}

	// process uncommitted ops for heights > finalCommitHeight
	for height, ops := range uncommittedOpsMap {
		if err := a.updateTrie(height, ops); err != nil {
			return fmt.Errorf("failed to update trie at height %d: %w", height, err)
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

	if err := a.UpdateLastCommitted(hash, height); err != nil {
		return err
	}
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

// UpdateLastCommitted sets the state to last committed hash and height
func (a *atomicTrie) UpdateLastCommitted(hash common.Hash, height uint64) error {
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

// Iterator returns a types.AtomicTrieIterator that iterates the trie from the given
// atomic trie root, starting at the specified height
func (a *atomicTrie) Iterator(root common.Hash, startHeight uint64) (AtomicTrieIterator, error) {
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

// ApplyToSharedMemory iterates over atomic ops indexed in the trie and applies them
// to sharedMemory, for heights less than or equal to [lastAcceptedBlock].
func (a *atomicTrie) ApplyToSharedMemory(lastAcceptedBlock uint64) error {
	lastAppliedToSharedMemoryHeight, err := a.lastAppliedToSharedMemoryHeight(lastAcceptedBlock)
	if err != nil {
		return err
	}
	startHeight := lastAppliedToSharedMemoryHeight + 1
	log.Info("ApplyToSharedMemory", "atomicRoot", a.lastCommittedHash, "lastAcceptedBlock", lastAcceptedBlock, "startHeight", startHeight)

	it, err := a.Iterator(a.lastCommittedHash, startHeight)
	if err != nil {
		return err
	}
	lastUpdate := time.Now()
	putRequests, removeRequests := 0, 0
	for it.Next() {
		height := it.BlockNumber()
		atomicOps := it.AtomicOps()

		if height > lastAcceptedBlock {
			break
		}

		putRequests += len(atomicOps.PutRequests)
		removeRequests += len(atomicOps.RemoveRequests)
		if time.Since(lastUpdate) > 10*time.Second {
			log.Info("atomic trie iteration", "height", height, "puts", putRequests, "removes", removeRequests)
			lastUpdate = time.Now()
		}

		// update last applied pointer
		if err := a.SetLastAppliedToSharedMemoryHeight(height); err != nil {
			return err
		}
		batch, err := a.db.CommitBatch()
		if err != nil {
			return err
		}
		// calling [sharedMemory.Apply] updates the last applied pointer atomically with the shared memory operation.
		if err = a.sharedMemory.Apply(map[ids.ID]*atomic.Requests{it.BlockchainID(): atomicOps}, batch); err != nil {
			return err
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	log.Info("finished applying atomic operations")
	if err := a.clearLastAppliedToSharedMemoryHeight(); err != nil {
		return err
	}
	return a.db.Commit()
}

// lastAppliedToSharedMemoryHeight returns the last height applied to the shared memory.
// In case lastAppliedToSharedMemoryKey is stored with a value of [height], the trie
// contains atomic ops [height+1, lastCommittedHeight) that need to be applied to shared memory.
// If the key is not present, all the atomic ops in the trie are already applied and this
// function returns lastAcceptedBlock.
func (a *atomicTrie) lastAppliedToSharedMemoryHeight(lastAcceptedBlock uint64) (uint64, error) {
	height, err := database.GetUInt64(a.metadataDB, lastAppliedToSharedMemoryKey)
	switch err {
	case database.ErrNotFound:
		return lastAcceptedBlock, nil
	case nil:
		return height, nil
	default:
		return 0, err
	}
}

// SetLastAppliedToSharedMemoryHeight marks the atomic trie as containing atomic ops
// that must be applied to shared memory (on initialization). Heights less than or
// equal to [height] must already be applied to shared memory.
func (a *atomicTrie) SetLastAppliedToSharedMemoryHeight(height uint64) error {
	return database.PutUInt64(a.metadataDB, lastAppliedToSharedMemoryKey, height)
}

func (a *atomicTrie) clearLastAppliedToSharedMemoryHeight() error {
	return a.metadataDB.Delete(lastAppliedToSharedMemoryKey)
}
