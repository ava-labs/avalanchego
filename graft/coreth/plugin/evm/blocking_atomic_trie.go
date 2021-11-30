// (c) 2020-2021, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"

	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/fastsync/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	commitHeightInterval = uint64(4096)
)

var (
	lastCommittedKey         = []byte("LastCommittedBlock")
	atomicTrieInitializedKey = []byte("atomicTrieInitialized")
)

type AtomicReqs map[ids.ID]*atomic.Requests
type AtomicIndexReq struct {
	height uint64
	ops    AtomicReqs
}

// blockingAtomicTrie implements the types.AtomicTrie interface
// using the eth trie.Trie implementation
type blockingAtomicTrie struct {
	commitHeightInterval uint64              // commit interval, same as commitHeightInterval by default
	db                   ethdb.KeyValueStore // Underlying database
	trieDB               *trie.Database      // Trie database
	trie                 *trie.Trie          // Atomic trie.Trie mapping key (height+blockchainID) and value (RLP encoded atomic.Requests)
	repo                 AtomicTxRepository
	lastCommittedHash    common.Hash // trie root hash of the most recent commit
	lastCommittedHeight  uint64      // index height of the most recent commit
	codec                codec.Manager
}

func NewBlockingAtomicTrie(db ethdb.KeyValueStore, repo AtomicTxRepository, codec codec.Manager) (types.AtomicTrie, error) {
	root, height, err := lastCommittedRootIfExists(db)
	if err != nil {
		return nil, err
	}

	triedb := trie.NewDatabase(db)
	t, err := trie.New(root, triedb)

	if err != nil {
		return nil, err
	}

	return &blockingAtomicTrie{
		commitHeightInterval: commitHeightInterval,
		db:                   db,
		trieDB:               triedb,
		trie:                 t,
		repo:                 repo,
		codec:                codec,
		lastCommittedHash:    root,
		lastCommittedHeight:  height,
	}, nil
}

// lastCommittedRootIfExists returns the last committed trie root and height if it exists
// else returns empty common.Hash{} and 0
// returns optional error if there are issues with the underlying data store
// or if values present in the database are not as expected
func lastCommittedRootIfExists(db ethdb.KeyValueStore) (common.Hash, uint64, error) {
	var root common.Hash
	var height uint64
	// read the last committed entry if exists and set root hash
	lastCommittedHeightBytes, err := db.Get(lastCommittedKey)
	if err != nil && err.Error() != database.ErrNotFound.Error() { // err type does not match database.ErrorNotFound so have to check `.Error()` instead
		return common.Hash{}, 0, err
	} else if err == nil || err.Error() != database.ErrNotFound.Error() {
		// entry is present, check length
		lastCommittedHeightBytesLen := len(lastCommittedHeightBytes)
		if lastCommittedHeightBytesLen != wrappers.LongLen {
			return common.Hash{}, 0, fmt.Errorf("expected value of lastCommittedKey to be %d but was %d", wrappers.LongLen, lastCommittedHeightBytesLen)
		}

		height = binary.BigEndian.Uint64(lastCommittedHeightBytes)

		hash, err := db.Get(lastCommittedHeightBytes)
		if err != nil {
			return common.Hash{}, 0, err
		}

		root = common.BytesToHash(hash)
	}
	return root, height, nil
}

// nearestCommitHeight given a block number calculates the nearest commit height such that the
// commit height is always less than blockNumber, commitHeight+commitInterval is greater
// than the blockNumber and commit height is completely divisible by commit interval
func nearestCommitHeight(blockNumber uint64, commitInterval uint64) uint64 {
	if blockNumber == 0 {
		return 0
	}
	return blockNumber - (blockNumber % commitInterval)
}

func (b *blockingAtomicTrie) isInitialized() (bool, error) {
	initialized := false
	initializedBytes, err := b.db.Get(atomicTrieInitializedKey)
	if err != nil && err.Error() != database.ErrNotFound.Error() {
		return false, err
	} else if err == nil || err.Error() != database.ErrNotFound.Error() {
		initializedBytesLen := len(initializedBytes)
		if initializedBytesLen != wrappers.BoolLen {
			return false, fmt.Errorf("expected atomicTrieInitialized value to be %d bytes long but was %d", wrappers.BoolLen, initializedBytesLen)
		}

		// based on packer UnpackBool implementation
		if initializedBytes[0] == 1 {
			initialized = true
		}
	}
	return initialized, nil
}

// Initialize initializes the atomic trie using the atomic repository height index iterating from the oldest to the
// lastAcceptedBlockNumber making a single commit at the most recent height divisible by the commitInterval
// Optionally returns an error
// Initialize only ever runs once during a node's lifetime. Subsequent updates to this trie are made using the
// Index call during evm.block.Accept().
func (b *blockingAtomicTrie) Initialize(lastAcceptedBlockNumber uint64, dbCommitFn func() error) error {
	initialized, err := b.isInitialized()
	if err != nil {
		return err
	} else if initialized {
		log.Info("atomic trie is already initialized")
		return nil
	}

	// commitHeight is the highest block that can be committed i.e. is divisible by b.commitHeightInterval
	commitHeight := nearestCommitHeight(lastAcceptedBlockNumber, b.commitHeightInterval)
	uncommittedOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests, lastAcceptedBlockNumber-commitHeight)

	iter := b.repo.IterateByHeight(nil)
	defer iter.Release()

	preCommitTxIndexed := 0
	postCommitTxIndexed := 0
	lastUpdate := time.Now()
	for iter.Next() {
		if err := iter.Error(); err != nil {
			return err
		}
		height := binary.BigEndian.Uint64(iter.Key())
		txs, err := ExtractAtomicTxs(iter.Value(), true, b.codec)
		if err != nil {
			return err
		}
		for _, tx := range txs {
			id, reqs, err := tx.Accept()
			ops := map[ids.ID]*atomic.Requests{id: reqs}
			if err != nil {
				return err
			}

			if height > commitHeight {
				uncommittedOpsMap[height] = ops
			} else {
				if err = b.index(height, ops); err != nil {
					return err
				}
				preCommitTxIndexed++
			}
		}
		if time.Since(lastUpdate) > 30*time.Second {
			log.Info("imported entries into atomic trie pre-commit", "entriesIndexed", preCommitTxIndexed)
			lastUpdate = time.Now()
		}
	}

	// skip commit in case of early height
	if lastAcceptedBlockNumber < b.commitHeightInterval {
		if dbCommitFn != nil {
			if err := dbCommitFn(); err != nil {
				return err
			}
		}
		return nil
	}
	// now that all heights < commitHeight have been processed
	// commit the trie
	hash, err := b.commit(commitHeight)
	if err != nil {
		return err
	}

	if dbCommitFn != nil {
		if err = dbCommitFn(); err != nil {
			return err
		}
	}

	log.Info("committed trie", "hash", hash, "height", commitHeight)

	// process uncommitted ops
	for height, ops := range uncommittedOpsMap {
		if err = b.index(height, ops); err != nil {
			return err
		}
		postCommitTxIndexed++
		if time.Since(lastUpdate) > 30*time.Second {
			log.Info("imported entries into atomic trie post-commit", "entriesIndexed", postCommitTxIndexed)
			lastUpdate = time.Now()
		}
	}

	// check if its eligible to commit the trie
	if lastAcceptedBlockNumber%b.commitHeightInterval == 0 {
		hash, err := b.commit(lastAcceptedBlockNumber)
		if err != nil {
			return err
		}
		log.Info("committed trie", "hash", hash, "height", lastAcceptedBlockNumber)
	}

	initializedBytes := []byte{1}
	if err = b.db.Put(atomicTrieInitializedKey, initializedBytes); err != nil {
		return err
	}

	if dbCommitFn != nil {
		if err = dbCommitFn(); err != nil {
			return err
		}
	}

	log.Info("index initialised", "preCommitEntriesIndexed", preCommitTxIndexed, "postCommitEntriesIndexed", postCommitTxIndexed)

	return nil
}

// Index updates the trie with entries in atomicOps
// Returns optional hash and optional error
// atomicOps is a map of blockchainID -> atomic.Requests
// A non-empty hash is returned when the height is within the commitInterval
// and the trie is committed
// This function updates the following:
// - index height (indexHeightKey) => [height]
// - heightBytes => trie root hash (if within commitInterval)
// - lastCommittedBlock => height (if trie was committed this time)
// If indexHeightKey is not set in the database, this function will
//   initialise it with [height] as the starting height - this *must* be zero in
//   that case
func (b *blockingAtomicTrie) Index(height uint64, atomicOps map[ids.ID]*atomic.Requests) (common.Hash, error) {
	_, lastCommittedHeight := b.LastCommitted()

	if height < lastCommittedHeight {
		return common.Hash{}, fmt.Errorf("height %d must be after last committed height %d", height, lastCommittedHeight)
	}

	nextCommitHeight := lastCommittedHeight + b.commitHeightInterval
	if height > nextCommitHeight {
		return common.Hash{}, fmt.Errorf("height %d not within the next commit height %d", height, nextCommitHeight)
	}

	if err := b.index(height, atomicOps); err != nil {
		return common.Hash{}, err
	}

	if height%b.commitHeightInterval == 0 {
		return b.commit(height)
	}
	return common.Hash{}, nil
}

func (b *blockingAtomicTrie) index(height uint64, atomicOps map[ids.ID]*atomic.Requests) error {
	return b.updateTrie(atomicOps, height)
}

// commit commits the underlying trie generating a trie root hash
// assumes that the caller is aware of the commit rules i.e. the height being within commitInterval
// returns the trie root from the commit + optional error
func (b *blockingAtomicTrie) commit(height uint64) (common.Hash, error) {
	l := log.New("func", "indexedAtomicTrie.index", "height", height)
	hash, err := b.commitTrie()
	if err != nil {
		return common.Hash{}, err
	}

	b.lastCommittedHash = hash
	l.Info("committed atomic trie", "hash", hash.String(), "height", height)
	if err = b.trieDB.Commit(hash, false, nil); err != nil {
		return common.Hash{}, err
	}

	// all good here, update the heightBytes
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	// now save the trie hash against the height it was committed at
	if err = b.db.Put(heightBytes, hash[:]); err != nil {
		return common.Hash{}, err
	}

	// update lastCommittedKey with the current height
	if err = b.db.Put(lastCommittedKey, heightBytes); err != nil {
		return common.Hash{}, err
	}

	b.lastCommittedHeight = height
	return hash, nil
}

func (b *blockingAtomicTrie) updateTrie(atomicOps map[ids.ID]*atomic.Requests, height uint64) error {
	for blockchainID, requests := range atomicOps {
		// value is RLP encoded atomic.Requests struct
		valueBytes, err := rlp.EncodeToBytes(*requests)
		if err != nil {
			// highly unlikely but possible if atomic.Element
			// has a change that is unsupported by the RLP encoder
			return err
		}

		// key is [height]+[blockchainID]
		keyPacker := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen+len(blockchainID[:]))}
		keyPacker.PackLong(height)
		keyPacker.PackFixedBytes(blockchainID[:])
		b.trie.Update(keyPacker.Bytes, valueBytes)
	}
	return nil
}

func (b *blockingAtomicTrie) commitTrie() (common.Hash, error) {
	hash, _, err := b.trie.Commit(nil)
	return hash, err
}

// LastCommitted returns the last committed trie hash, block height, and an optional error
func (b *blockingAtomicTrie) LastCommitted() (common.Hash, uint64) {
	return b.lastCommittedHash, b.lastCommittedHeight
}

// Iterator returns a types.AtomicTrieIterator that iterates the trie from the given
// atomic trie root
func (b *blockingAtomicTrie) Iterator(root common.Hash) (types.AtomicTrieIterator, error) {
	t, err := trie.New(root, b.trieDB)
	if err != nil {
		return nil, err
	}

	iter := trie.NewIterator(t.NodeIterator(nil))
	return NewAtomicTrieIterator(iter), iter.Err
}

func (b *blockingAtomicTrie) TrieDB() *trie.Database {
	return b.trieDB
}

func (b *blockingAtomicTrie) Root(height uint64) (common.Hash, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	hash, err := b.db.Get(heightBytes)
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(hash), err
}
