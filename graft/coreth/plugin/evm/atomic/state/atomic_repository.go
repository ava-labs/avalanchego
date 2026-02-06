// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	repoCommitSizeCap = 10 * units.MiB
)

var (
	atomicTxIDDBPrefix         = []byte("atomicTxDB")
	atomicHeightTxDBPrefix     = []byte("atomicHeightTxDB")
	atomicRepoMetadataDBPrefix = []byte("atomicRepoMetadataDB")
	atomicTrieStoragePrefix    = []byte("atomicTrieDB")
	atomicTrieMetaDBPrefix     = []byte("atomicTrieMetaDB")

	appliedSharedMemoryCursorKey = []byte("atomicTrieLastAppliedToSharedMemory")
	maxIndexedHeightKey          = []byte("maxIndexedAtomicTxHeight")
	// Historically used to track the completion of a migration
	// bonusBlocksRepairedKey     = []byte("bonusBlocksRepaired")
)

// AtomicRepository manages the database interactions for atomic operations.
type AtomicRepository struct {
	// [acceptedAtomicTxDB] maintains an index of [txID] => [height]+[atomic tx] for all accepted atomic txs.
	acceptedAtomicTxDB database.Database

	// [acceptedAtomicTxByHeightDB] maintains an index of [height] => [atomic txs] for all accepted block heights.
	acceptedAtomicTxByHeightDB database.Database

	// [atomicRepoMetadataDB] maintains a single key-value pair which tracks the height up to which the atomic repository
	// has indexed.
	atomicRepoMetadataDB database.Database

	metadataDB database.Database // Underlying database containing the atomic trie metadata

	atomicTrieStorage database.Database // Raw database storage for atomic trie (unwrapped)

	// [db] is used to commit to the underlying versiondb.
	db *versiondb.Database

	// Use this codec for serializing
	codec codec.Manager
}

func NewAtomicTxRepository(
	db *versiondb.Database, codec codec.Manager, lastAcceptedHeight uint64,
) (*AtomicRepository, error) {
	repo := &AtomicRepository{
		atomicTrieStorage:          prefixdb.New(atomicTrieStoragePrefix, db),
		metadataDB:                 prefixdb.New(atomicTrieMetaDBPrefix, db),
		acceptedAtomicTxDB:         prefixdb.New(atomicTxIDDBPrefix, db),
		acceptedAtomicTxByHeightDB: prefixdb.New(atomicHeightTxDBPrefix, db),
		atomicRepoMetadataDB:       prefixdb.New(atomicRepoMetadataDBPrefix, db),
		codec:                      codec,
		db:                         db,
	}
	if err := repo.initializeHeightIndex(lastAcceptedHeight); err != nil {
		return nil, err
	}
	return repo, nil
}

// initializeHeightIndex initializes the atomic repository and takes care of any required migration from the previous database
// format which did not have a height -> txs index.
func (a *AtomicRepository) initializeHeightIndex(lastAcceptedHeight uint64) error {
	startTime := time.Now()
	lastLogTime := startTime

	// [lastTxID] will be initialized to the last transaction that we indexed
	// if we are part way through a migration.
	var lastTxID ids.ID
	indexHeightBytes, err := a.atomicRepoMetadataDB.Get(maxIndexedHeightKey)
	switch err {
	case nil:
	case database.ErrNotFound:
	default: // unexpected value in the database
		return fmt.Errorf("found invalid value at max indexed height: %v", indexHeightBytes)
	}

	switch len(indexHeightBytes) {
	case 0:
		log.Info("Initializing atomic transaction repository from scratch")
	case common.HashLength: // partially initialized
		lastTxID, err = ids.ToID(indexHeightBytes)
		if err != nil {
			return err
		}
		log.Info("Initializing atomic transaction repository from txID", "lastTxID", lastTxID)
	case wrappers.LongLen: // already initialized
		return nil
	default: // unexpected value in the database
		return fmt.Errorf("found invalid value at max indexed height: %v", indexHeightBytes)
	}

	// Iterate from [lastTxID] to complete the re-index -> generating an index
	// from height to a slice of transactions accepted at that height
	iter := a.acceptedAtomicTxDB.NewIteratorWithStart(lastTxID[:])
	defer iter.Release()

	indexedTxs := 0

	// Keep track of the size of the currently pending writes
	pendingBytesApproximation := 0
	for iter.Next() {
		// iter.Value() consists of [height packed as uint64] + [tx serialized as packed []byte]
		iterValue := iter.Value()
		if len(iterValue) < wrappers.LongLen {
			return fmt.Errorf("atomic tx DB iterator value had invalid length (%d) < (%d)", len(iterValue), wrappers.LongLen)
		}
		heightBytes := iterValue[:wrappers.LongLen]

		// Get the tx iter is pointing to, len(txs) == 1 is expected here.
		txBytes := iterValue[wrappers.LongLen+wrappers.IntLen:]
		tx, err := atomic.ExtractAtomicTx(txBytes, a.codec)
		if err != nil {
			return err
		}

		// Check if there are already transactions at [height], to ensure that we
		// add [txs] to the already indexed transactions at [height] instead of
		// overwriting them.
		if err := a.appendTxToHeightIndex(heightBytes, tx); err != nil {
			return err
		}
		lastTxID = tx.ID()
		pendingBytesApproximation += len(txBytes)

		// call commitFn to write to underlying DB if we have reached
		// [commitSizeCap]
		if pendingBytesApproximation > repoCommitSizeCap {
			if err := a.atomicRepoMetadataDB.Put(maxIndexedHeightKey, lastTxID[:]); err != nil {
				return err
			}
			if err := a.db.Commit(); err != nil {
				return err
			}
			log.Info("Committing work initializing the atomic repository", "lastTxID", lastTxID, "pendingBytesApprox", pendingBytesApproximation)
			pendingBytesApproximation = 0
		}
		indexedTxs++
		// Periodically log progress
		if time.Since(lastLogTime) > 15*time.Second {
			lastLogTime = time.Now()
			log.Info("Atomic repository initialization", "indexedTxs", indexedTxs)
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("atomic tx DB iterator errored while initializing atomic trie: %w", err)
	}

	// Updated the value stored [maxIndexedHeightKey] to be the lastAcceptedHeight
	indexedHeight := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(indexedHeight, lastAcceptedHeight)
	if err := a.atomicRepoMetadataDB.Put(maxIndexedHeightKey, indexedHeight); err != nil {
		return err
	}

	log.Info("Completed atomic transaction repository migration", "lastAcceptedHeight", lastAcceptedHeight, "duration", time.Since(startTime))
	return a.db.Commit()
}

// GetIndexHeight returns the last height that was indexed by the atomic repository
func (a *AtomicRepository) GetIndexHeight() (uint64, error) {
	indexHeightBytes, err := a.atomicRepoMetadataDB.Get(maxIndexedHeightKey)
	if err != nil {
		return 0, err
	}

	if len(indexHeightBytes) != wrappers.LongLen {
		return 0, fmt.Errorf("unexpected length for indexHeightBytes %d", len(indexHeightBytes))
	}
	indexHeight := binary.BigEndian.Uint64(indexHeightBytes)
	return indexHeight, nil
}

// GetByTxID queries [acceptedAtomicTxDB] for the [txID], parses a [*atomic.Tx] object
// if an entry is found, and returns it with the block height the atomic tx it
// represents was accepted on, along with an optional error.
func (a *AtomicRepository) GetByTxID(txID ids.ID) (*atomic.Tx, uint64, error) {
	indexedTxBytes, err := a.acceptedAtomicTxDB.Get(txID[:])
	if err != nil {
		return nil, 0, err
	}

	if len(indexedTxBytes) < wrappers.LongLen {
		return nil, 0, fmt.Errorf("acceptedAtomicTxDB entry too short: %d", len(indexedTxBytes))
	}

	// value is stored as [height]+[tx bytes], decompose with a packer.
	packer := wrappers.Packer{Bytes: indexedTxBytes}
	height := packer.UnpackLong()
	txBytes := packer.UnpackBytes()
	tx, err := atomic.ExtractAtomicTx(txBytes, a.codec)
	if err != nil {
		return nil, 0, err
	}

	return tx, height, nil
}

// GetByHeight returns all atomic txs processed on block at [height].
// Returns [database.ErrNotFound] if there are no atomic transactions indexed at [height].
// Note: if [height] is below the last accepted height, then this means that there were
// no atomic transactions in the block accepted at [height].
// If [height] is greater than the last accepted height, then this will always return
// [database.ErrNotFound]
func (a *AtomicRepository) GetByHeight(height uint64) ([]*atomic.Tx, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	return a.getByHeightBytes(heightBytes)
}

func (a *AtomicRepository) getByHeightBytes(heightBytes []byte) ([]*atomic.Tx, error) {
	txsBytes, err := a.acceptedAtomicTxByHeightDB.Get(heightBytes)
	if err != nil {
		return nil, err
	}
	return atomic.ExtractAtomicTxsBatch(txsBytes, a.codec)
}

// Write updates indexes maintained on atomic txs, so they can be queried
// by txID or height. This method must be called only once per height,
// and [txs] must include all atomic txs for the block accepted at the
// corresponding height.
func (a *AtomicRepository) Write(height uint64, txs []*atomic.Tx) error {
	return a.write(height, txs, false)
}

// WriteBonus is similar to Write, except the [txID] => [height] is not
// overwritten if already exists.
func (a *AtomicRepository) WriteBonus(height uint64, txs []*atomic.Tx) error {
	return a.write(height, txs, true)
}

func (a *AtomicRepository) write(height uint64, txs []*atomic.Tx, bonus bool) error {
	if len(txs) > 1 {
		// txs should be stored in order of txID to ensure consistency
		// with txs initialized from the txID index.
		copyTxs := make([]*atomic.Tx, len(txs))
		copy(copyTxs, txs)
		utils.Sort(copyTxs)
		txs = copyTxs
	}
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	// Skip adding an entry to the height index if [txs] is empty.
	if len(txs) > 0 {
		for _, tx := range txs {
			if bonus {
				switch _, _, err := a.GetByTxID(tx.ID()); err {
				case nil:
					// avoid overwriting existing value if [bonus] is true
					continue
				case database.ErrNotFound:
					// no existing value to overwrite, proceed as normal
				default:
					// unexpected error
					return err
				}
			}
			if err := a.indexTxByID(heightBytes, tx); err != nil {
				return err
			}
		}
		if err := a.indexTxsAtHeight(heightBytes, txs); err != nil {
			return err
		}
	}

	// Update the index height regardless of if any atomic transactions
	// were present at [height].
	return a.atomicRepoMetadataDB.Put(maxIndexedHeightKey, heightBytes)
}

// indexTxByID writes [tx] into the [acceptedAtomicTxDB] stored as
// [height] + [tx bytes]
func (a *AtomicRepository) indexTxByID(heightBytes []byte, tx *atomic.Tx) error {
	txBytes, err := a.codec.Marshal(atomic.CodecVersion, tx)
	if err != nil {
		return err
	}

	// map txID => [height]+[tx bytes]
	heightTxPacker := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen+wrappers.IntLen+len(txBytes))}
	heightTxPacker.PackFixedBytes(heightBytes)
	heightTxPacker.PackBytes(txBytes)
	txID := tx.ID()

	if err := a.acceptedAtomicTxDB.Put(txID[:], heightTxPacker.Bytes); err != nil {
		return err
	}

	return nil
}

// indexTxsAtHeight adds [height] -> [txs] to the [acceptedAtomicTxByHeightDB]
func (a *AtomicRepository) indexTxsAtHeight(heightBytes []byte, txs []*atomic.Tx) error {
	txsBytes, err := a.codec.Marshal(atomic.CodecVersion, txs)
	if err != nil {
		return err
	}
	if err := a.acceptedAtomicTxByHeightDB.Put(heightBytes, txsBytes); err != nil {
		return err
	}
	return nil
}

// appendTxToHeightIndex retrieves the transactions stored at [heightBytes] and appends
// [tx] to the slice of transactions stored there.
// This function is used while initializing the atomic repository to re-index the atomic transactions
// by txID into the height -> txs index.
func (a *AtomicRepository) appendTxToHeightIndex(heightBytes []byte, tx *atomic.Tx) error {
	txs, err := a.getByHeightBytes(heightBytes)
	if err != nil && err != database.ErrNotFound {
		return err
	}

	// Iterate over the existing transactions to ensure we do not add a
	// duplicate to the index.
	for _, existingTx := range txs {
		if existingTx.ID() == tx.ID() {
			return nil
		}
	}

	txs = append(txs, tx)
	return a.indexTxsAtHeight(heightBytes, txs)
}

// IterateByHeight returns an iterator beginning at [height].
// Note [height] must be greater than 0 since we assume there are no
// atomic txs in genesis.
func (a *AtomicRepository) IterateByHeight(height uint64) database.Iterator {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	return a.acceptedAtomicTxByHeightDB.NewIteratorWithStart(heightBytes)
}
