package evm

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/fastsync/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	commitHeightInterval = uint64(4096)
	errEntryNotFound     = "not found"
)

var (
	lastCommittedKey = []byte("LastCommittedBlock")
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
}

func NewBlockingAtomicTrie(db ethdb.KeyValueStore, repo AtomicTxRepository) (types.AtomicTrie, error) {
	var root common.Hash
	// read the last committed entry if exists and automagically set root hash
	lastCommittedHeightBytes, err := db.Get(lastCommittedKey)
	if err == nil {
		hash, err := db.Get(lastCommittedHeightBytes)
		if err == nil {
			root = common.BytesToHash(hash)
		}
	}
	// if [err] is other than not found, return it.
	if err != nil && err.Error() != errEntryNotFound {
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
	}, nil
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

func (i *blockingAtomicTrie) Initialize(lastAcceptedBlockNumber uint64, dbCommitFn func() error) error {
	commitHeight := nearestCommitHeight(lastAcceptedBlockNumber, i.commitHeightInterval)
	uncommittedOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests, lastAcceptedBlockNumber-commitHeight)

	iter := i.repo.Iterate()
	defer iter.Release()

	preCommitTxIndexed := 0
	postCommitTxIndexed := 0
	lastUpdate := time.Now()
	for iter.Next() {
		if err := iter.Error(); err != nil {
			return err
		}
		packer := wrappers.Packer{Bytes: iter.Value()}
		height := packer.UnpackLong()
		txBytes := packer.UnpackBytes()
		tx, err := i.repo.ParseTxBytes(txBytes)
		if err != nil {
			return err
		}

		ops, err := tx.AtomicOps()
		if err != nil {
			return err
		}

		if height > commitHeight {
			// add it to the map to index later
			// make sure we merge transactions at same height
			if opsMap, exists := uncommittedOpsMap[height]; !exists {
				// create a new entry if it does not exist
				uncommittedOpsMap[height] = ops
			} else {
				// for existing entry at same height, we merge the atomic ops
				for blockchainID, atomicRequests := range ops {
					if requests, exists := opsMap[blockchainID]; !exists {
						opsMap[blockchainID] = atomicRequests
					} else {
						requests.PutRequests = append(requests.PutRequests, atomicRequests.PutRequests...)
						requests.RemoveRequests = append(requests.RemoveRequests, atomicRequests.RemoveRequests...)
					}
				}
			}
		} else {
			if err = i.index(height, ops); err != nil {
				return err
			}
			preCommitTxIndexed++
		}
		if time.Since(lastUpdate) > 30*time.Second {
			log.Info("imported entries into atomic trie pre-commit", "preCommitEntriesIndexed", preCommitTxIndexed)
			lastUpdate = time.Now()
		}
	}

	// skip commit in case of early height
	if lastAcceptedBlockNumber < i.commitHeightInterval {
		if dbCommitFn != nil {
			if err := dbCommitFn(); err != nil {
				return err
			}
		}
		return nil
	}
	// now that all heights < commitHeight have been processed
	// commit the trie
	hash, err := i.commit(commitHeight)
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
		if err = i.index(height, ops); err != nil {
			return err
		}
		postCommitTxIndexed++
		if time.Since(lastUpdate) > 30*time.Second {
			log.Info("imported entries into atomic trie post-commit", "postCommitEntriesIndexed", postCommitTxIndexed)
			lastUpdate = time.Now()
		}
	}

	// check if its eligible to commit the trie
	if lastAcceptedBlockNumber%i.commitHeightInterval == 0 {
		hash, err := i.commit(lastAcceptedBlockNumber)
		if err != nil {
			return err
		}
		log.Info("committed trie", "hash", hash, "height", lastAcceptedBlockNumber)
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
func (i *blockingAtomicTrie) Index(height uint64, atomicOps map[ids.ID]*atomic.Requests) (common.Hash, error) {
	_, lastCommittedHeight, err := i.LastCommitted()
	if err != nil {
		return common.Hash{}, err
	}

	if height < lastCommittedHeight {
		return common.Hash{}, fmt.Errorf("height %d must be after last committed height %d", height, lastCommittedHeight)
	}

	nextCommitHeight := lastCommittedHeight + i.commitHeightInterval
	if height > nextCommitHeight {
		return common.Hash{}, fmt.Errorf("height %d not within the next commit height %d", height, nextCommitHeight)
	}

	if err = i.index(height, atomicOps); err != nil {
		return common.Hash{}, err
	}
	if height%i.commitHeightInterval == 0 {
		return i.commit(height)
	}
	return common.Hash{}, nil
}

func (i *blockingAtomicTrie) index(height uint64, atomicOps map[ids.ID]*atomic.Requests) error {
	return i.updateTrie(atomicOps, height)
}

func (i *blockingAtomicTrie) commit(height uint64) (common.Hash, error) {
	l := log.New("func", "indexedAtomicTrie.index", "height", height)
	if height%i.commitHeightInterval != 0 {
		return common.Hash{}, fmt.Errorf("atomic tx trie commit height must be divisable by %d (got %d)", i.commitHeightInterval, height)
	}

	hash, err := i.commitTrie()
	if err != nil {
		return common.Hash{}, err
	}
	l.Info("committed atomic trie", "hash", hash.String(), "height", height)
	if err = i.trieDB.Commit(hash, false, nil); err != nil {
		return common.Hash{}, err
	}
	// all good here, update the hightBytes
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	// now save the trie hash against the height it was committed at
	if err = i.db.Put(heightBytes, hash[:]); err != nil {
		return common.Hash{}, err
	}
	// update lastCommittedKey with the current height
	if err = i.db.Put(lastCommittedKey, heightBytes); err != nil {
		return common.Hash{}, err
	}
	return hash, nil
}

func (i *blockingAtomicTrie) updateTrie(atomicOps map[ids.ID]*atomic.Requests, height uint64) error {
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
		i.trie.Update(keyPacker.Bytes, valueBytes)
	}
	return nil
}

func (i *blockingAtomicTrie) commitTrie() (common.Hash, error) {
	hash, _, err := i.trie.Commit(nil)
	return hash, err
}

// LastCommitted returns the last committed trie hash, block height, and an optional error
func (i *blockingAtomicTrie) LastCommitted() (common.Hash, uint64, error) {
	heightBytes, err := i.db.Get(lastCommittedKey)
	if err != nil && err.Error() == errEntryNotFound {
		// trie has not been committed yet
		return common.Hash{}, 0, nil
	} else if err != nil {
		return common.Hash{}, 0, err
	}

	height := binary.BigEndian.Uint64(heightBytes)
	hash, err := i.db.Get(heightBytes)
	return common.BytesToHash(hash), height, err
}

// Iterator returns a types.AtomicTrieIterator that iterates the trie from the given
// atomic root hash
func (i *blockingAtomicTrie) Iterator(hash common.Hash) (types.AtomicTrieIterator, error) {
	t, err := trie.New(hash, i.trieDB)
	if err != nil {
		return nil, err
	}

	iter := trie.NewIterator(t.NodeIterator(nil))
	return NewAtomicTrieIterator(iter), iter.Err
}

func (i *blockingAtomicTrie) TrieDB() *trie.Database {
	return i.trieDB
}

func (i *blockingAtomicTrie) Root(height uint64) (common.Hash, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	hash, err := i.db.Get(heightBytes)
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(hash), err
}
