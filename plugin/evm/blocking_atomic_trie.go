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
	historicalTrieRoots  = 8
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
	pendingWrites        []*AtomicIndexReq
	initialisedChan      chan struct{}
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
		pendingWrites:        make([]*AtomicIndexReq, 0),
		initialisedChan:      make(chan struct{}),
	}, nil
}

func (i *blockingAtomicTrie) Initialize(lastAcceptedBlockNumber uint64, dbCommitFn func() error) error {
	return i.initialize(lastAcceptedBlockNumber, dbCommitFn)
}

// initialize populates blockingAtomicTrie, doing a prefix scan on [acceptedHeightAtomicTxDB]
// from current position up to [lastAcceptedBlockNumber]. Optionally returns error.
func (i *blockingAtomicTrie) initialize(lastAcceptedBlockNumber uint64, dbCommitFn func() error) error {
	transactionsIndexed := uint64(0)
	transactionsIndexedDirect := uint64(0)
	startTime := time.Now()

	lastCommittedHash, nextHeight, err := i.LastCommitted()
	if err != nil {
		return err
	}
	if (lastCommittedHash != common.Hash{}) && nextHeight > 0 {
		// index has been initialized.
		if lastAcceptedBlockNumber > nextHeight && lastAcceptedBlockNumber-nextHeight > i.commitHeightInterval {
			log.Error("index lagging by unexpected amount", "lastAcceptedBlockNumber", lastAcceptedBlockNumber, "nextHeight", nextHeight)
			return fmt.Errorf("index lagging by unexpected amount")
		}
		log.Info("atomic trie already initialized", "lastAcceptedBlockNumber", lastAcceptedBlockNumber, "nextHeight", nextHeight)
		return nil
	}

	// When [initialize] returns, [historicalTrieRoot] trie roots will have been created.
	// Each root is the hash of the trie containing all atomic operations requested by
	// atomic txs accepted on blocks [0..height] (inclusive).
	// Roots will be created starting at [lastCommitHeight], at [i.commitHeightInterval]
	// intervals.
	// Atomic operations for txs accepted on blocks <= [directToTrieHeight] can be indexed
	// directly, and others will be buffered into
	lastCommitHeight := lastAcceptedBlockNumber - lastAcceptedBlockNumber%i.commitHeightInterval
	rootCount := uint64(historicalTrieRoots)
	if lastAcceptedBlockNumber < historicalTrieRoots*i.commitHeightInterval {
		rootCount = lastAcceptedBlockNumber / i.commitHeightInterval
	}
	directToTrieHeight := lastCommitHeight - (rootCount-1)*i.commitHeightInterval
	buckets := make([]map[uint64]AtomicReqs, rootCount)

	it := i.repo.Iterate()
	logger := NewProgressLogger(10 * time.Second)

	for it.Next() {
		if err := it.Error(); err != nil {
			return err
		}
		packer := wrappers.Packer{Bytes: it.Value()}
		height := packer.UnpackLong()
		txBytes := packer.UnpackBytes()
		tx, err := i.repo.ParseTxBytes(txBytes)
		if err != nil {
			return err
		}

		// NOTE: this logic only works assuming a single atomic tx accepted per block height
		transactionsIndexed++
		ops, err := tx.AtomicOps()
		if err != nil {
			return err
		}
		if height <= directToTrieHeight {
			// index the merged atomic requests against the height
			if err := i.index(height, ops); err != nil {
				return err
			}
			transactionsIndexedDirect++
		} else {
			bucketIdx := (height - directToTrieHeight - 1) / i.commitHeightInterval
			if buckets[bucketIdx] == nil {
				buckets[bucketIdx] = make(map[uint64]AtomicReqs)
			}
			buckets[bucketIdx][height] = ops
		}
		logger.Info("atomic trie init progress", "transactionsIndexed", transactionsIndexed)
	}

	// all known txs have processed.
	hash, err := i.commit(directToTrieHeight)
	if err != nil {
		return err
	}
	log.Info("committed directToTrieHeight", "hash", hash, "directToTrieHeight", directToTrieHeight, "transactionsIndexedDirect", transactionsIndexedDirect)

	for idx, bucket := range buckets {
		log.Info("processing bucket", "idx", idx, "len", len(bucket))
		for height, ops := range bucket {
			if err := i.index(height, ops); err != nil {
				return err
			}
		}
		if idx == len(buckets)-1 {
			// atomic ops from txs in last bucket will be committed at next commitHeightInterval, skip commit here
			continue
		}
		hash, err := i.commit(directToTrieHeight + uint64(idx+1)*i.commitHeightInterval)
		if err != nil {
			return err
		}
		log.Info("committed bucket", "idx", idx, "len", len(bucket), "hash", hash)
	}

	if dbCommitFn != nil {
		log.Info("committing DB")
		if err := dbCommitFn(); err != nil {
			log.Error("unable to commit DB", "err", err)
			return err
		}
	}
	log.Info("atomic trie initialisation complete", "time", time.Since(startTime))
	close(i.initialisedChan)
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
	select {
	case <-i.initialisedChan:
	// initialization is complete, proceed normally
	default:
		i.pendingWrites = append(i.pendingWrites, &AtomicIndexReq{height: height, ops: atomicOps})
		return common.Hash{}, nil
	}

	// first time called after initialization is complete will flush pendingWrites
	for _, req := range i.pendingWrites {
		if err := i.index(req.height, req.ops); err != nil {
			return common.Hash{}, err
		}
	}
	i.pendingWrites = nil

	// normal, ongoing behavior: index and commit if at [commitHeightInterval]
	if err := i.index(height, atomicOps); err != nil {
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
		return common.Hash{}, fmt.Errorf("atomic tx trie commit height must be divisable by 4096 (got %v)", height)
	}

	hash, err := i.commitTrie()
	if err != nil {
		return common.Hash{}, err
	}
	l.Info("committed atomic trie", "hash", hash, "height", height)
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
