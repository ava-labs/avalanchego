// (c) 2020-2021, Ava Labs, Inc.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	commitSizeCap = 10 * units.MiB
)

var (
	atomicTxIDDBPrefix     = []byte("atomicTxDB")
	atomicHeightTxDBPrefix = []byte("atomicHeightTxDB")
	maxIndexedHeightKey    = []byte("maxIndexedAtomicTxHeight")
)

// AtomicTxRepository defines an entity that manages storage and indexing of
// atomic transactions
type AtomicTxRepository interface {
	GetIndexHeight() (uint64, bool, error)
	GetByTxID(txID ids.ID) (*Tx, uint64, error)
	GetByHeight(height uint64) ([]*Tx, error)
	Write(height uint64, txs []*Tx) error
	IterateByTxID() database.Iterator
	IterateByHeight([]byte) database.Iterator
}

// atomicTxRepository is a prefixdb implementation of the AtomicTxRepository interface
type atomicTxRepository struct {
	// [acceptedAtomicTxDB] maintains an index of [txID] => [height]+[atomic tx] for all accepted atomic txs.
	acceptedAtomicTxDB database.Database

	// [acceptedAtomicTxByHeightDB] maintains an index of [height] => [atomic txs] for all accepted block heights.
	acceptedAtomicTxByHeightDB database.Database

	// This db is used to store [maxIndexedHeightKey] to avoid interfering with the iterators over the atomic transaction DBs.
	db *versiondb.Database

	// Use this codec for serializing
	codec codec.Manager
}

func NewAtomicTxRepository(db *versiondb.Database, codec codec.Manager) (AtomicTxRepository, error) {
	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	acceptedAtomicTxByHeightDB := prefixdb.New(atomicHeightTxDBPrefix, db)

	repo := &atomicTxRepository{
		acceptedAtomicTxDB:         acceptedAtomicTxDB,
		acceptedAtomicTxByHeightDB: acceptedAtomicTxByHeightDB,
		codec:                      codec,
		db:                         db,
	}
	return repo, repo.initialize()
}

func (a *atomicTxRepository) initialize() error {
	startTime := time.Now()
	indexHeight := uint64(0)
	indexHeight, exists, err := a.GetIndexHeight()
	if err != nil {
		return err
	}
	if exists {
		log.Info("updating atomic tx repository height index", "lastHeight", indexHeight)
	} else {
		log.Info("initialising atomic tx repository height index for the first time")
	}

	txIndexed, txSkipped := 0, 0
	iter := a.acceptedAtomicTxDB.NewIterator()
	defer iter.Release()

	// Keep track of the max height we have seen
	newMaxHeight := indexHeight

	// Remember which heights we processed so we can read existing txs if we encounter the same height twice.
	// TODO usage of seenHeights is broken when there are restarts, so we need to decide how to fix this.
	seenHeights := make(map[uint64]struct{})
	lastLogTime := startTime

	// Keep track of the size of pending writes to be committed
	totalApproxCommitBytes := 0

	for iter.Next() {
		if err := iter.Error(); err != nil {
			return fmt.Errorf("atomic tx DB iterator errored while initializing atomic trie: %w", err)
		}
		// Periodically log progress
		if time.Since(lastLogTime) > 15*time.Second {
			log.Info("updating tx repository height index", "indexed", txIndexed, "skipped", txSkipped)
			lastLogTime = time.Now()
		}

		// iter.Value() consists of [height packed as uint64] + [tx serialized as packed []byte]
		iterValue := iter.Value()
		heightBytes := iterValue[:wrappers.LongLen]
		height := binary.BigEndian.Uint64(heightBytes)
		if height < indexHeight {
			txSkipped++
			continue
		}

		// Get the tx iter is pointing to, len(txs) == 1 is expected here.
		txBytes := iterValue[wrappers.LongLen+wrappers.IntLen:]
		txs, err := ExtractAtomicTx(txBytes, a.codec)
		if err != nil {
			return err
		}

		// If this height is already processed, get the existing transactions
		// at [height] so that they can be re-indexed.
		if _, exists := seenHeights[height]; exists {
			existingTxs, err := a.GetByHeight(height)
			if err != nil {
				return err
			}
			txs = append(existingTxs, txs...)
		}
		seenHeights[height] = struct{}{}

		txsBytes, err := a.codec.Marshal(codecVersion, txs)
		if err != nil {
			return err
		}
		if err := a.acceptedAtomicTxByHeightDB.Put(heightBytes, txsBytes); err != nil {
			return err
		}
		totalApproxCommitBytes += len(txBytes)

		// call commitFn to write to underlying DB if needed
		if totalApproxCommitBytes > commitSizeCap {
			if err := a.db.Commit(); err != nil {
				return err
			}
			totalApproxCommitBytes = 0
		}

		// update max height if necessary
		if height > newMaxHeight {
			newMaxHeight = height
		}
		txIndexed++
	}

	newMaxHeightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(newMaxHeightBytes, newMaxHeight)
	if err := a.db.Put(maxIndexedHeightKey, newMaxHeightBytes); err != nil {
		return err
	}

	if err := a.db.Commit(); err != nil {
		return err
	}

	log.Info("initialized atomic tx repository", "indexHeight", indexHeight, "maxHeight", newMaxHeight, "duration", time.Since(startTime), "txIndexed", txIndexed, "txSkipped", txSkipped)
	return nil
}

// GetIndexHeight returns:
// - index height
// - whether the index has been initialized
// - optional error
func (a *atomicTxRepository) GetIndexHeight() (uint64, bool, error) {
	indexHeightBytes, err := a.db.Get(maxIndexedHeightKey)
	if err == database.ErrNotFound {
		return 0, false, nil
	} else if err != nil {
		return 0, false, err
	}

	if len(indexHeightBytes) != wrappers.LongLen {
		return 0, false, fmt.Errorf("unexpected length for indexHeightBytes %d", len(indexHeightBytes))
	}
	indexHeight := binary.BigEndian.Uint64(indexHeightBytes)
	return indexHeight, true, nil
}

// GetByTxID queries [acceptedAtomicTxDB] for the [txID], parses a [*Tx] object
// if an entry is found, and returns it with the block height the atomic tx it
// represents was accepted on, along with an optional error.
func (a *atomicTxRepository) GetByTxID(txID ids.ID) (*Tx, uint64, error) {
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
	txs, err := ExtractAtomicTx(txBytes, a.codec)
	if err != nil {
		return nil, 0, err
	}
	if len(txs) != 1 {
		return nil, 0, fmt.Errorf("unexpected len for ExtractAtomicTxs return value %d", len(txs))
	}

	return txs[0], height, nil
}

// GetByHeight returns all atomic txs processed on block at [height].
func (a *atomicTxRepository) GetByHeight(height uint64) ([]*Tx, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	txsBytes, err := a.acceptedAtomicTxByHeightDB.Get(heightBytes)
	if err != nil {
		return nil, err
	}
	return ExtractAtomicTxsBatch(txsBytes, a.codec)
}

// Write updates indexes maintained on atomic txs, so they can be queried
// by txID or height. This method must be called only once per height,
// and [txs] must include all atomic txs for the block accepted at the
// corresponding height.
func (a *atomicTxRepository) Write(height uint64, txs []*Tx) error {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	for _, tx := range txs {
		txBytes, err := a.codec.Marshal(codecVersion, tx)
		if err != nil {
			return err
		}

		// map txID => [height]+[tx bytes]
		heightTxPacker := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen+wrappers.IntLen+len(txBytes))}
		heightTxPacker.PackLong(height)
		heightTxPacker.PackBytes(txBytes)
		txID := tx.ID()

		if err := a.acceptedAtomicTxDB.Put(txID[:], heightTxPacker.Bytes); err != nil {
			return err
		}
	}

	txsBytes, err := a.codec.Marshal(codecVersion, txs)
	if err != nil {
		return err
	}
	if err := a.acceptedAtomicTxByHeightDB.Put(heightBytes, txsBytes); err != nil {
		return err
	}

	return a.db.Put(maxIndexedHeightKey, heightBytes)
}

func (a *atomicTxRepository) IterateByTxID() database.Iterator {
	return a.acceptedAtomicTxDB.NewIterator()
}

func (a *atomicTxRepository) IterateByHeight(heightBytes []byte) database.Iterator {
	return a.acceptedAtomicTxByHeightDB.NewIteratorWithStart(heightBytes)
}
