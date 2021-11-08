package evm

import (
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/coreth/fastsync/types"

	"github.com/ava-labs/coreth/fastsync/facades"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/trie"
	gethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const commitHeightInterval = uint64(4096)
const errEntryNotFound = "not found"

var (
	indexHeightKey   = []byte("IndexHeight")
	lastCommittedKey = []byte("LastCommittedBlock")
)

// indexedAtomicTrie implements the types.AtomicTrie interface
// using the eth trie.Trie implementation
type indexedAtomicTrie struct {
	commitHeightInterval uint64              // commit interval, same as commitHeightInterval by default
	db                   ethdb.KeyValueStore // Underlying database
	trieDB               *trie.Database      // Trie database
	trie                 *trie.Trie          // Atomic trie.Trie mapping key (height+blockchainID) and value (RLP encoded atomic.Requests)
}

func NewIndexedAtomicTrie(db ethdb.KeyValueStore) (types.AtomicTrie, error) {
	var root gethCommon.Hash
	// read the last committed entry if exists and automagically set root hash
	lastCommittedHeightBytes, err := db.Get(lastCommittedKey)
	if err == nil {
		hash, err := db.Get(lastCommittedHeightBytes)
		if err == nil {
			root = gethCommon.BytesToHash(hash)
		}
	}

	// if the error is other than the database entry not found error, return it as error
	if err != nil && err.Error() != errEntryNotFound {
		// some error other than not found
		return nil, err
	}

	triedb := trie.NewDatabase(db)
	t, err := trie.New(root, triedb)
	if err != nil {
		return nil, err
	}
	return &indexedAtomicTrie{
		commitHeightInterval: commitHeightInterval,
		db:                   db,
		trieDB:               triedb,
		trie:                 t,
	}, nil
}

// Initialize initialises the atomic trie index for a specified [chain]
// Uses the getAtomicTxFn to get the atomic Tx to index
func (i *indexedAtomicTrie) Initialize(chain facades.ChainFacade, dbCommitFn func() error, getAtomicTxFn func(blk facades.BlockFacade) (map[ids.ID]*atomic.Requests, error)) error {
	// get the current indexer height, whether we've initialized and error if any in accessing the database
	lastIndexedHeight, initialized, err := i.Height()
	if err != nil {
		return err
	}

	// get the last accepted block height from the chain
	lastAcceptedBlockHeight := chain.LastAcceptedBlock().NumberU64()
	log.Info("updating atomic trie index", "pendingBlocks", lastAcceptedBlockHeight-lastIndexedHeight)
	// start syncing if DB is not initialized OR we're out of sync
	if !initialized || lastIndexedHeight != lastAcceptedBlockHeight {
		var blk facades.BlockFacade

		// if index is at genesis keep last indexed height as is (because uint can't be -1 for non indexed)
		// increment it for all other cases because we index the block that is one higher than the last indexed
		if lastIndexedHeight > 0 {
			// we index the next block
			lastIndexedHeight++
		}

		// fetch the block and loop until we're at the last accepted block height
		blk = chain.GetBlockByNumber(lastIndexedHeight)
		for lastIndexedHeight <= lastAcceptedBlockHeight {
			atomicOperations, err := getAtomicTxFn(blk)
			if err != nil {
				return err
			}

			// index atomicOperations (which may be nil) at the block height
			// because the index needs to increment to track how many blocks have been
			// indexed
			hash, err := i.Index(blk.NumberU64(), atomicOperations)
			if err != nil {
				return err
			}

			// call the DB commit if we just hit our commitInterval and committed the trie
			if hash != (gethCommon.Hash{}) {
				if err := dbCommitFn(); err != nil {
					log.Error("failed to commit DB", "hash", hash, "height", lastIndexedHeight, "err", err)
					return err
				}
			}

			// periodically log progress every 10k blocks
			if lastIndexedHeight%10000 == 0 {
				log.Info("indexed atomic transactions", "indexedHeight", lastIndexedHeight, "lastAcceptedBlockHeight", lastAcceptedBlockHeight, "remaining", lastAcceptedBlockHeight-lastIndexedHeight)
			}

			// update the last accepted block height from the chain (as it moves)
			// and increment the last indexed height
			lastAcceptedBlockHeight = chain.LastAcceptedBlock().NumberU64()
			lastIndexedHeight++

			// only get the next block if we're less than or equal to the last accepted block height
			// because get block for height > last accepted won't work
			if lastIndexedHeight <= lastAcceptedBlockHeight {
				blk = chain.GetBlockByNumber(lastIndexedHeight)
			}
		}

		// now that we're at last accepted block, commit the DB
		if err := dbCommitFn(); err != nil {
			return err
		}
	}
	return nil
}

// Height returns the current index height, whether the index is initialized and an optional error
func (i *indexedAtomicTrie) Height() (uint64, bool, error) {
	heightBytes, err := i.db.Get(indexHeightKey)
	if err != nil && err.Error() == errEntryNotFound {
		// trie has not been committed yet
		return 0, false, nil
	} else if err != nil {
		return 0, true, err
	}

	height := binary.BigEndian.Uint64(heightBytes)
	return height, true, err
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
func (i *indexedAtomicTrie) Index(height uint64, atomicOps map[ids.ID]*atomic.Requests) (gethCommon.Hash, error) {
	l := log.New("func", "indexedAtomicTrie.Index", "height", height)

	// get the current index height from the index DB
	currentHeight, exists, err := i.Height()
	if err != nil {
		// some error other than bytes not being present in the DB
		return gethCommon.Hash{}, err
	}

	currentHeightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(currentHeightBytes, currentHeight)

	if err != nil {
		// some error other than bytes not being present in the DB
		return gethCommon.Hash{}, err
	}

	if !exists {
		// this can happen when the indexer is running for the first time
		if height != 0 {
			// when the indexer is being indexed for the first time, the first
			// height is expected to be of the genesis block, which is expected
			// to be zero. This doesn't really affect the indexer but is a safety
			// measure to catch if something unexpected is going on.
			return gethCommon.Hash{}, fmt.Errorf("invalid starting height for a new index, must be exactly 0, is %d", height)
		}
	} else {
		currentHeight++

		// make sure currentHeight + 1 = height that we're about to index because we don't want gaps
		if currentHeight != height {
			// index is inconsistent, we cannot have missing entries so reject it
			return gethCommon.Hash{}, fmt.Errorf("inconsistent atomic trie index, currentHeight=%d, height=%d", currentHeight, height)
		}

		// all good here, update the currentHeightBytes
		binary.BigEndian.PutUint64(currentHeightBytes, currentHeight)
	}

	// now flush the uncommitted to the atomic trie
	for blockchainID, requests := range atomicOps {
		// value is RLP encoded atomic.Requests struct
		valueBytes, err := rlp.EncodeToBytes(*requests)
		if err != nil {
			// highly unlikely but possible if atomic.Element
			// has a change that is unsupported by the RLP encoder
			return gethCommon.Hash{}, err
		}

		// key is [currentHeightBytes]+[blockchainIDBytes]
		keyBytes := make([]byte, 0, wrappers.LongLen+len(blockchainID[:]))
		keyBytes = append(keyBytes, currentHeightBytes...)
		keyBytes = append(keyBytes, blockchainID[:]...)

		i.trie.Update(keyBytes, valueBytes)
	}

	// update the index height for next time
	if err = i.db.Put(indexHeightKey, currentHeightBytes); err != nil {
		return gethCommon.Hash{}, err
	}

	// check if we're in the commitInterval
	if height%i.commitHeightInterval != 0 {
		// we're not in the commit interval, pass
		return gethCommon.Hash{}, nil
	}

	// we're in the commit interval, we must commit the trie
	hash, _, err := i.trie.Commit(nil)
	if err != nil {
		return gethCommon.Hash{}, err
	}
	l.Info("committed atomic trie", "hash", hash)
	if err = i.trieDB.Commit(hash, false, nil); err != nil {
		return gethCommon.Hash{}, err
	}

	// now save the trie hash against the height it was committed at
	if err = i.db.Put(currentHeightBytes, hash[:]); err != nil {
		return gethCommon.Hash{}, err
	}

	// update lastCommittedKey with the current height
	if err = i.db.Put(lastCommittedKey, currentHeightBytes); err != nil {
		return gethCommon.Hash{}, err
	}

	return hash, nil
}

// LastCommitted returns the last committed trie hash, block height and an optional error
func (i *indexedAtomicTrie) LastCommitted() (gethCommon.Hash, uint64, error) {
	heightBytes, err := i.db.Get(lastCommittedKey)
	if err != nil && err.Error() == errEntryNotFound {
		// trie has not been committed yet
		return gethCommon.Hash{}, 0, nil
	} else if err != nil {
		return gethCommon.Hash{}, 0, err
	}

	height := binary.BigEndian.Uint64(heightBytes)
	hash, err := i.db.Get(heightBytes)
	return gethCommon.BytesToHash(hash), height, err
}

// Iterator returns a types.AtomicIterator that iterates the trie from the given
// atomic root hash
func (i *indexedAtomicTrie) Iterator(hash gethCommon.Hash) (types.AtomicIterator, error) {
	t, err := trie.New(hash, i.trieDB)
	if err != nil {
		return nil, err
	}

	iter := trie.NewIterator(t.NodeIterator(nil))
	return NewAtomicIterator(iter), iter.Err
}
