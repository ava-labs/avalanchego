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
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	atomicTxIDDBPrefix     = []byte("atomicTxDB")
	atomicHeightTxDBPrefix = []byte("atomicHeightTxDB")
	maxIndexedHeightKey    = []byte("maxIndexedAtomicTxHeight")
)

// AtomicTxRepository defines an entity that manages storage and indexing of
// atomic transactions
type AtomicTxRepository interface {
	Initialize(apricotPhase5Height uint64) error
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

	// Only used to store [maxIndexedHeightKey]
	db database.Database

	// Use this codec for serializing
	codec codec.Manager
}

func NewAtomicTxRepository(db database.Database, codec codec.Manager) AtomicTxRepository {
	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	acceptedAtomicTxByHeightDB := prefixdb.New(atomicHeightTxDBPrefix, db)

	return &atomicTxRepository{
		acceptedAtomicTxDB:         acceptedAtomicTxDB,
		acceptedAtomicTxByHeightDB: acceptedAtomicTxByHeightDB,
		codec:                      codec,
		db:                         db,
	}
}

func (a *atomicTxRepository) Initialize(apricotPhase5Height uint64) error {
	startTime := time.Now()
	indexHeight := uint64(0)
	exists, indexHeight, err := a.getIndexHeight()
	if err != nil {
		return nil
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
	seenHeights := make(map[uint64]struct{})
	lastLogTime := startTime

	// Keep track of the size of pending writes to be committed
	approxCommitBytes := make(map[uint64]int)
	totalApproxCommitBytes := 0
	commitSizeCap := 10 * 1024 * 1024

	for iter.Next() {
		// Periodically log progress
		if time.Since(lastLogTime) > 15*time.Second {
			log.Info("updating tx repository height index", "indexed", txIndexed, "skipped", txSkipped)
			lastLogTime = time.Now()
		}

		// iter.Value() consists of [height packed as uint64] + [tx serialized as packed []byte]
		heightBytes := iter.Value()[:wrappers.LongLen]
		height := binary.BigEndian.Uint64(heightBytes)
		if height < indexHeight {
			txSkipped++
			continue
		}

		// Get the tx iter is pointing to, len(txs) == 1 is expected here.
		txBytes := iter.Value()[wrappers.LongLen+wrappers.IntLen:]
		txs, err := ExtractAtomicTxs(txBytes, false, a.codec)
		if err != nil {
			return err
		}

		// Past apricotPhase5 we allow multiple txs per block
		// This ensures that we keep the memory footprint low
		// by not storing block heights for blocks prior
		if height >= apricotPhase5Height {
			// If this height is already processed, get existing txs for that height.
			if _, exists := seenHeights[height]; exists {
				existingTxs, err := a.GetByHeight(height)
				if err != nil {
					return err
				}
				txs = append(existingTxs, txs...)
			}
			seenHeights[height] = struct{}{}
		}

		txsBytes, err := a.codec.Marshal(codecVersion, txs)
		if err != nil {
			return err
		}
		if err := a.acceptedAtomicTxByHeightDB.Put(heightBytes, txsBytes); err != nil {
			return err
		}
		approxCommitBytes[height] = len(txBytes)
		totalApproxCommitBytes += len(txBytes)

		// call commitFn to write to underlying DB if needed
		if commitFn != nil && totalApproxCommitBytes > commitSizeCap {
			if err := commitFn(); err != nil {
				return err
			}
			approxCommitBytes = make(map[uint64]int)
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
	if commitFn != nil {
		if err := commitFn(); err != nil {
			return err
		}
	}

	log.Info("initialized atomic tx repository", "indexHeight", indexHeight, "maxHeight", newMaxHeight, "duration", time.Since(startTime), "txIndexed", txIndexed, "txSkipped", txSkipped)
	return nil
}

func (a *atomicTxRepository) getIndexHeight() (bool, uint64, error) {
	exists, err := a.db.Has(maxIndexedHeightKey)
	if err != nil {
		return false, 0, err
	}
	if !exists {
		return exists, 0, nil
	}

	indexHeightBytes, err := a.db.Get(maxIndexedHeightKey)
	if err != nil {
		return exists, 0, err
	}
	if len(indexHeightBytes) != wrappers.LongLen {
		return exists, 0, fmt.Errorf("unexpected length for indexHeightBytes %d", len(indexHeightBytes))
	}
	indexHeight := binary.BigEndian.Uint64(indexHeightBytes)
	return exists, indexHeight, nil
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
	txs, err := ExtractAtomicTxs(txBytes, false, a.codec)
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
	return ExtractAtomicTxs(txsBytes, true, a.codec)
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
