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
	maxIndexedHeightKey    = []byte("maxIndexedHeight")
)

// AtomicTxRepository defines an entity that manages storage and indexing of
// atomic transactions
type AtomicTxRepository interface {
	Initialize() error
	GetByTxID(txID ids.ID) (*Tx, uint64, error)
	GetByHeight(height uint64) ([]*Tx, error)
	Write(height uint64, txs []*Tx) error
	IterateByTxID() database.Iterator
	IterateByHeight(heightBytes []byte) database.Iterator
}

// atomicTxRepository is a prefixdb implementation of the AtomicTxRepository interface
type atomicTxRepository struct {
	// [acceptedAtomicTxDB] maintains an index of [txID] => [height]+[atomic tx] for all accepted atomic txs.
	acceptedAtomicTxDB         database.Database
	acceptedAtomicTxByHeightDB database.Database
	codec                      codec.Manager
}

func NewAtomicTxRepository(db database.Database, codec codec.Manager) AtomicTxRepository {
	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	acceptedAtomicTxByHeightDB := prefixdb.New(atomicHeightTxDBPrefix, db)

	return &atomicTxRepository{
		acceptedAtomicTxDB:         acceptedAtomicTxDB,
		acceptedAtomicTxByHeightDB: acceptedAtomicTxByHeightDB,
		codec:                      codec,
	}
}

func (a *atomicTxRepository) Initialize() error {
	exists, err := a.acceptedAtomicTxByHeightDB.Has(maxIndexedHeightKey)
	if err != nil {
		return err
	}
	if !exists {
		return a.createAtomicTxByHeightIndex()
	}
	return nil
}

func (a *atomicTxRepository) createAtomicTxByHeightIndex() error {
	startTime := time.Now()

	indexHeight := uint64(0)
	var indexHeightBytes []byte
	exists, err := a.acceptedAtomicTxByHeightDB.Has(maxIndexedHeightKey)
	if err != nil {
		return err
	}

	if exists {
		indexHeightBytes, err = a.acceptedAtomicTxByHeightDB.Get(maxIndexedHeightKey)
		if err != nil {
			return err
		}
		if len(indexHeightBytes) == wrappers.LongLen {
			indexHeight = binary.BigEndian.Uint64(indexHeightBytes)
		}
		log.Info("updating atomic tx repository height index", "lastHeight", indexHeight)
	} else {
		log.Info("initialising atomic tx repository height index for the first time")
	}

	txIndexed, txSkipped := 0, 0
	iter := a.acceptedAtomicTxDB.NewIterator()
	defer iter.Release()

	// Since leveldb orders keys and we're using BigEndian we should get the natural order at the DB level
	// automatically when using the iterator later
	newMaxHeight := indexHeight
	newMaxHeightBytes := indexHeightBytes

	// In the 1st pass, we optimistically assume there is a maximum of 1 atomic tx / height (since multiple
	// atomic tx is new at this time and this index is initialized once). If a height occurs twice, remember
	// it and write the remaining txs as a second pass.
	seenHeights := make(map[uint64]struct{})
	pendingWrites := make(map[uint64][]*Tx)

	heightIndexBatch := a.acceptedAtomicTxByHeightDB.NewBatch()
	logger := newProgressLogger(15 * time.Second)
	for iter.Next() {
		logger.Info("updating tx repository height index", "indexed", txIndexed, "skipped", txSkipped)

		heightBytes := iter.Value()[:wrappers.LongLen]
		height := binary.BigEndian.Uint64(heightBytes)
		if exists && height < indexHeight {
			txSkipped++
			continue
		}

		// TODO: possibly replace with packer
		txBytes := iter.Value()[wrappers.LongLen+wrappers.IntLen:]
		tx, err := ParseTxBytes(txBytes, a.codec)
		if err != nil {
			return err
		}

		if _, exists := seenHeights[height]; exists {
			pendingWrites[height] = append(pendingWrites[height], tx)
			continue
		}
		seenHeights[height] = struct{}{}

		txs := []*Tx{tx}
		txsBytes, err := a.codec.Marshal(codecVersion, txs)
		if err != nil {
			return err
		}

		if err := heightIndexBatch.Put(heightBytes, txsBytes); err != nil {
			return err
		}

		// update our height record
		if height > newMaxHeight {
			newMaxHeight = height
			newMaxHeightBytes = heightBytes
		}

		txIndexed++
	}

	// Commit writes from 1st pass so they are visible during 2nd pass.
	if err := heightIndexBatch.Write(); err != nil {
		return err
	}

	// 2nd pass writes out pendingWrites to disk.
	heightIndexBatch = a.acceptedAtomicTxByHeightDB.NewBatch()
	for height, additionalTxs := range pendingWrites {
		heightBytes := make([]byte, wrappers.LongLen)
		binary.BigEndian.PutUint64(heightBytes, height)

		txs, err := a.GetByHeight(height)
		if err != nil {
			return err
		}
		if len(txs) == 0 {
			return fmt.Errorf("expected atomic txs to be found at height: %d", height)
		}
		txs = append(txs, additionalTxs...)
		txsBytes, err := a.codec.Marshal(codecVersion, txs)
		if err != nil {
			return err
		}
		if err := heightIndexBatch.Put(heightBytes, txsBytes); err != nil {
			return err
		}
		txIndexed++
	}

	if err := heightIndexBatch.Put(maxIndexedHeightKey, newMaxHeightBytes); err != nil {
		return err
	}
	if err := heightIndexBatch.Write(); err != nil {
		return err
	}

	log.Info("initialized atomic tx repository", "indexHeight", indexHeight, "maxHeight", newMaxHeight, "duration", time.Since(startTime), "txIndexed", txIndexed, "txSkipped", txSkipped)
	return nil
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
	tx, err := ParseTxBytes(txBytes, a.codec)
	if err != nil {
		return nil, 0, err
	}

	return tx, height, nil
}

// GetByHeight returns all atomic txs processed on block at [height].
func (a *atomicTxRepository) GetByHeight(height uint64) ([]*Tx, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	txsBytes, err := a.acceptedAtomicTxByHeightDB.Get(heightBytes)
	if err != nil {
		return nil, err
	}
	return ParseTxsBytes(txsBytes, a.codec)
}

// ParseTxBytes parses [bytes] to a [*Tx] object using [codec].
func ParseTxBytes(bytes []byte, c codec.Manager) (*Tx, error) {
	tx := &Tx{}
	if _, err := c.Unmarshal(bytes, tx); err != nil {
		return nil, fmt.Errorf("problem parsing atomic tx from db: %w", err)
	}
	if err := tx.Sign(c, nil); err != nil {
		return nil, fmt.Errorf("problem initializing atomic tx from db: %w", err)
	}
	return tx, nil
}

// ParseTxsBytes parses [bytes] to a [[]*Tx] slice using [codec].
func ParseTxsBytes(bytes []byte, c codec.Manager) ([]*Tx, error) {
	txs := []*Tx{}
	if _, err := c.Unmarshal(bytes, &txs); err != nil {
		return nil, fmt.Errorf("problem parsing atomic txs from db: %w", err)
	}
	for _, tx := range txs {
		if err := tx.Sign(c, nil); err != nil {
			return nil, fmt.Errorf("problem initializing atomic tx from db: %w", err)
		}
	}
	return txs, nil
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
		heightTxPacker := wrappers.Packer{Bytes: make([]byte, 12+len(txBytes))}
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

	return a.acceptedAtomicTxByHeightDB.Put(maxIndexedHeightKey, heightBytes)
}

func (a *atomicTxRepository) IterateByTxID() database.Iterator {
	return a.acceptedAtomicTxDB.NewIterator()
}

func (a *atomicTxRepository) IterateByHeight(heightBytes []byte) database.Iterator {
	return a.acceptedAtomicTxByHeightDB.NewIteratorWithStart(heightBytes)
}

// TODO: consider moving this to a utils package and adding other methods
type progressLogger struct {
	lastUpdate time.Time
	interval   time.Duration
}

func newProgressLogger(interval time.Duration) *progressLogger {
	return &progressLogger{
		lastUpdate: time.Now(),
		interval:   interval,
	}
}

func (pl *progressLogger) Info(msg string, args ...interface{}) {
	if time.Since(pl.lastUpdate) > 15*time.Second {
		log.Info(msg, args...)
		pl.lastUpdate = time.Now()
	}
}
