package evm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/rlp"
	atomic2 "go.uber.org/atomic"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/fastsync/facades"
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
	initialised          *atomic2.Bool
}

func NewBlockingAtomicTrie(db ethdb.KeyValueStore) (types.AtomicTrie, error) {
	var root common.Hash
	// read the last committed entry if exists and automagically set root hash
	lastCommittedHeightBytes, err := db.Get(lastCommittedKey)
	if err == nil {
		hash, err := db.Get(lastCommittedHeightBytes)
		if err == nil {
			root = common.BytesToHash(hash)
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

	return &blockingAtomicTrie{
		commitHeightInterval: commitHeightInterval,
		db:                   db,
		trieDB:               triedb,
		trie:                 t,
		initialised:          atomic2.NewBool(false),
	}, nil
}

func (i *blockingAtomicTrie) Initialize(chain facades.ChainFacade, dbCommitFn func() error, acceptedHeightAtomicTxDB database.Database, codec codec.Manager) <-chan error {
	resultChan := make(chan error, 1)
	go i.initialize(chain, dbCommitFn, acceptedHeightAtomicTxDB, codec, resultChan)
	return resultChan

}

// initialize blockingly initializes the blockingAtomicTrie using the acceptedHeightAtomicTxDB
// and lastAccepted from the chain
// Publishes any errors to the resultChan
func (i *blockingAtomicTrie) initialize(chain facades.ChainFacade, dbCommitFn func() error, acceptedHeightAtomicTxDB database.Database, codec codec.Manager, resultChan chan<- error) {
	defer close(resultChan)
	lastAccepted := chain.LastAcceptedBlock()
	iter := acceptedHeightAtomicTxDB.NewIterator()
	transactionsIndexed := uint64(0)
	startTime := time.Now()
	lastUpdate := time.Now()
	for iter.Next() && iter.Error() == nil {
		heightBytes := iter.Key()
		if len(heightBytes) != wrappers.LongLen ||
			bytes.Equal(heightBytes, heightAtomicTxDBInitializedKey) {
			// this is metadata key, skip it
			continue
		}

		height := binary.BigEndian.Uint64(heightBytes)
		if height > lastAccepted.NumberU64() {
			// skip tx if height is > last accepted
			continue
		}

		txBytes := iter.Value()

		tx := &Tx{}
		if _, err := codec.Unmarshal(txBytes, tx); err != nil {
			log.Error("problem parsing atomic transaction from db", "err", err)
			resultChan <- err
			return
		}
		if err := tx.Sign(codec, nil); err != nil {
			log.Error("problem initializing atomic transaction from DB", "err", err)
			resultChan <- err
			return
		}
		ops, err := tx.AtomicOps()
		if err != nil {
			log.Error("problem getting atomic ops", "err", err)
			resultChan <- err
			return
		}

		binary.BigEndian.PutUint64(heightBytes, height)
		err = i.updateTrie(ops, heightBytes)
		if err != nil {
			log.Error("problem indexing atomic ops", "err", err)
			resultChan <- err
			return
		}

		transactionsIndexed++
		if time.Since(lastUpdate) > 30*time.Second {
			log.Info("atomic trie init progress", "indexedTransactions", transactionsIndexed)
			lastUpdate = time.Now()
		}
	}

	if iter.Error() != nil {
		log.Error("error iterating data", "err", iter.Error())
		resultChan <- iter.Error()
		return
	}

	log.Info("done updating trie, setting index height", "height", lastAccepted.NumberU64(), "duration", time.Since(startTime))
	if err := i.setIndexHeight(lastAccepted.NumberU64()); err != nil {
		log.Error("error setting index height", "height", lastAccepted.NumberU64())
		resultChan <- err
		return
	}

	log.Info("committing trie")
	hash, err := i.commitTrie()
	if err != nil {
		log.Error("error committing trie post init")
		resultChan <- err
		return
	}

	log.Info("trie committed", "hash", hash, "height", lastAccepted.NumberU64(), "time", time.Since(startTime))

	if dbCommitFn != nil {
		log.Info("committing DB")
		if err := dbCommitFn(); err != nil {
			log.Error("unable to commit DB")
			resultChan <- err
			return
		}
	}

	log.Info("atomic trie initialisation complete", "time", time.Since(startTime))

	i.initialised.Store(true)
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
	if !i.initialised.Load() {
		// ignoring request because index has not yet been initialised
		// the ongoing Initialize() will catch up to this height later
		return common.Hash{}, nil
	}

	return i.index(height, atomicOps)
}

func (i *blockingAtomicTrie) index(height uint64, atomicOps map[ids.ID]*atomic.Requests) (common.Hash, error) {
	l := log.New("func", "indexedAtomicTrie.index", "height", height)

	// get the current index height from the index DB
	currentHeight, exists, err := i.Height()
	if err != nil {
		// some error other than bytes not being present in the DB
		return common.Hash{}, err
	}

	currentHeightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(currentHeightBytes, currentHeight)

	if err != nil {
		// some error other than bytes not being present in the DB
		return common.Hash{}, err
	}

	if !exists {
		// this can happen when the indexer is running for the first time
		if height != 0 {
			// when the indexer is being indexed for the first time, the first
			// height is expected to be of the genesis block, which is expected
			// to be zero. This doesn't really affect the indexer but is a safety
			// measure to catch if something unexpected is going on.
			return common.Hash{}, fmt.Errorf("invalid starting height for a new index, must be exactly 0, is %d", height)
		}
	} else {
		currentHeight++

		// make sure currentHeight + 1 = height that we're about to index because we don't want gaps
		if currentHeight != height {
			// index is inconsistent, we cannot have missing entries so skip it, Initialize() will get to it
			log.Debug("skipped Index(...) call for higher block height because index is not yet complete", "currentHeight", currentHeight, "height", height)
			return common.Hash{}, nil
		}

		// all good here, update the currentHeightBytes
		binary.BigEndian.PutUint64(currentHeightBytes, currentHeight)
	}

	// now flush the uncommitted to the atomic trie
	err = i.updateTrie(atomicOps, currentHeightBytes)
	if err != nil {
		return common.Hash{}, err
	}

	// update the index height for next time
	if err = i.db.Put(indexHeightKey, currentHeightBytes); err != nil {
		return common.Hash{}, err
	}

	// check if we're in the commitInterval
	if height%i.commitHeightInterval != 0 {
		// we're not in the commit interval, pass
		return common.Hash{}, nil
	}

	// we're in the commit interval, we must commit the trie
	hash, _, err := i.trie.Commit(nil)
	if err != nil {
		return common.Hash{}, err
	}
	l.Info("committed atomic trie", "hash", hash)
	if err = i.trieDB.Commit(hash, false, nil); err != nil {
		return common.Hash{}, err
	}

	// now save the trie hash against the height it was committed at
	if err = i.db.Put(currentHeightBytes, hash[:]); err != nil {
		return common.Hash{}, err
	}

	// update lastCommittedKey with the current height
	if err = i.db.Put(lastCommittedKey, currentHeightBytes); err != nil {
		return common.Hash{}, err
	}

	return hash, nil
}

func (i *blockingAtomicTrie) updateTrie(atomicOps map[ids.ID]*atomic.Requests, currentHeightBytes []byte) error {
	for blockchainID, requests := range atomicOps {
		// value is RLP encoded atomic.Requests struct
		valueBytes, err := rlp.EncodeToBytes(*requests)
		if err != nil {
			// highly unlikely but possible if atomic.Element
			// has a change that is unsupported by the RLP encoder
			return err
		}

		// key is [currentHeightBytes]+[blockchainIDBytes]
		keyBytes := make([]byte, 0, wrappers.LongLen+len(blockchainID[:]))
		keyBytes = append(keyBytes, currentHeightBytes...)
		keyBytes = append(keyBytes, blockchainID[:]...)

		i.trie.Update(keyBytes, valueBytes)
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

// LastCommitted returns the last committed trie hash, block height and an optional error
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

// Iterator returns a types.AtomicIterator that iterates the trie from the given
// atomic root hash
func (i *blockingAtomicTrie) Iterator(hash common.Hash) (types.AtomicIterator, error) {
	t, err := trie.New(hash, i.trieDB)
	if err != nil {
		return nil, err
	}

	iter := trie.NewIterator(t.NodeIterator(nil))
	return NewAtomicIterator(iter), iter.Err
}

// Height returns the current index height, whether the index is initialized and an optional error
func (i *blockingAtomicTrie) Height() (uint64, bool, error) {
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
