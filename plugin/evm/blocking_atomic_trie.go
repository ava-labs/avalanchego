package evm

import (
	"bytes"
	"encoding/binary"
	"time"

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

const commitHeightInterval = uint64(4096)
const errEntryNotFound = "not found"

var (
	indexHeightKey   = []byte("IndexHeight")
	lastCommittedKey = []byte("LastCommittedBlock")
)

// blockingAtomicTrie implements the types.AtomicTrie interface
// using the eth trie.Trie implementation
type blockingAtomicTrie struct {
	commitHeightInterval uint64              // commit interval, same as commitHeightInterval by default
	db                   ethdb.KeyValueStore // Underlying database
	trieDB               *trie.Database      // Trie database
	trie                 *trie.Trie          // Atomic trie.Trie mapping key (height+blockchainID) and value (RLP encoded atomic.Requests)
	repo                 AtomicTxRepository
	initialisedChan      chan error
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
		initialisedChan:      make(chan error, 1),
		repo:                 repo,
	}, nil
}

func (i *blockingAtomicTrie) Initialize(lastAcceptedBlockNumber uint64, dbCommitFn func() error, codec codec.Manager) <-chan error {
	go func() {
		i.initialisedChan <- i.initialize(lastAcceptedBlockNumber, dbCommitFn, codec)
		close(i.initialisedChan)
	}()
	return i.initialisedChan
}

// initialize populates blockingAtomicTrie, doing a prefix scan on [acceptedHeightAtomicTxDB]
// from current position up to [lastAcceptedBlockNumber]. Optionally returns error.
func (i *blockingAtomicTrie) initialize(lastAcceptedBlockNumber uint64, dbCommitFn func() error, codec codec.Manager) error {
	transactionsIndexed := uint64(0)
	startTime := time.Now()

	_, nextHeight, err := i.LastCommitted()
	if err != nil {
		return err
	}
	// keep track of pending atomic txs at the same height
	pendingAtomicOps := make(map[ids.ID]*atomic.Requests)
	appendAtomicOps := func(ops map[ids.ID]*atomic.Requests) {
		for blockchainID, reqs := range ops {
			if currentReqs, exists := pendingAtomicOps[blockchainID]; exists {
				currentReqs.PutRequests = append(currentReqs.PutRequests, reqs.PutRequests...)
				currentReqs.RemoveRequests = append(currentReqs.RemoveRequests, reqs.RemoveRequests...)
			} else {
				pendingAtomicOps[blockchainID] = reqs
			}
		}
	}
	indexAtomicOpsOrNil := func(height uint64) {
		if len(pendingAtomicOps) > 0 {
			i.index(height, pendingAtomicOps)
			pendingAtomicOps = make(map[ids.ID]*atomic.Requests)
		} else {
			i.index(height, nil)
		}
	}

	// start iteration at [nextHeight], all previous heights are already indexed
	it := i.repo.IterateByHeight(nextHeight)
	logger := NewProgressLogger(10 * time.Second)
	for it.Next() {
		if err := it.Error(); err != nil {
			return err
		}
		// TODO: add error if unexpected key found
		heightBytes := it.Key()
		if len(heightBytes) != wrappers.LongLen ||
			bytes.Equal(heightBytes, heightAtomicTxDBInitializedKey) {
			// this is metadata key, skip it
			continue
		}
		height := binary.BigEndian.Uint64(heightBytes)

		// catch up trie index to height observed in iterator
		for ; nextHeight < height; nextHeight++ {
			indexAtomicOpsOrNil(nextHeight)
		}
		// height == nextHeight
		txs, err := i.repo.ParseTxsBytes(it.Value())
		if err != nil {
			return err
		}
		for _, tx := range txs {
			transactionsIndexed++
			ops, err := tx.AtomicOps()
			if err != nil {
				return err
			}
			appendAtomicOps(ops)
		}
		// remember the atomic ops for this tx in [pendingAtomicOps]
		// we will call [index] on them when either:
		// - a greater height is observed from iterating
		// - iteration is complete
		logger.Info("atomic trie init progress", "transactionsIndexed", transactionsIndexed)
	}
	// make sure [index] is called up to [lastAcceptedBlockNumber]
	for ; nextHeight <= lastAcceptedBlockNumber; nextHeight++ {
		indexAtomicOpsOrNil(nextHeight)
	}

	if dbCommitFn != nil {
		log.Info("committing DB")
		if err := dbCommitFn(); err != nil {
			log.Error("unable to commit DB", "err", err)
			return err
		}
	}
	log.Info("atomic trie initialisation complete", "time", time.Since(startTime))
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
	/*
		TODO: not sure if we really need to support loading this async anymore.
		if we do, the assumption is that index will be called on each height exactly once, so we must be careful
		to not break that assumption.
		We can't really rely on Initialize because the iterator behavior does not guarantee including keys
		that are added after instantiating the iterator.
		if !i.initialised.Load() {
			// ignoring request because index has not yet been initialised
			// the ongoing Initialize() will catch up to this height later
			return common.Hash{}, nil
		}*/

	return i.index(height, atomicOps)
}

// TODO: is the return value useful? most of the time we return common.Hash{}
func (i *blockingAtomicTrie) index(height uint64, atomicOps map[ids.ID]*atomic.Requests) (common.Hash, error) {
	err := i.updateTrie(atomicOps, height)
	if err != nil {
		return common.Hash{}, err
	}
	// early return if block height is not divisible by [commitHeightInterval]
	if height%i.commitHeightInterval != 0 {
		return i.commit(height)
	}
	return common.Hash{}, nil
}

func (i *blockingAtomicTrie) commit(height uint64) (common.Hash, error) {
	l := log.New("func", "indexedAtomicTrie.index", "height", height)
	// TODO: check the assumption height%i.commitHeightInterval == 0

	hash, err := i.commitTrie()
	if err != nil {
		return common.Hash{}, err
	}
	l.Info("committed atomic trie", "hash", hash)
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

func (i *blockingAtomicTrie) setIndexHeight(height uint64) error {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	return i.db.Put(indexHeightKey, heightBytes)
}

func (i *blockingAtomicTrie) commitTrie() (common.Hash, error) {
	hash, _, err := i.trie.Commit(nil)
	return hash, err
}

// LastCommitted returns the last committed trie hash, next indexable block height, and an optional error
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
	return common.BytesToHash(hash), height + 1, err
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
