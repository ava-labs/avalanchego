package evm

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/log"

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
	IterateByHeight(height uint64) database.Iterator
}

// atomicTxRepository is a prefixdb implementation of the AtomicTxRepository interface
type atomicTxRepository struct {
	// [acceptedAtomicTxDB] maintains an index of [txID] => [height]+[atomic tx] for all accepted atomic txs.
	acceptedAtomicTxDB         database.Database
	acceptedAtomicTxByHeightDB database.Database
}

func NewAtomicTxRepository(db database.Database) AtomicTxRepository {
	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	acceptedAtomicTxByHeightDB := prefixdb.New(atomicHeightTxDBPrefix, db)

	return &atomicTxRepository{
		acceptedAtomicTxDB:         acceptedAtomicTxDB,
		acceptedAtomicTxByHeightDB: acceptedAtomicTxByHeightDB,
	}
}

func (a *atomicTxRepository) Initialize() error {
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

	// Since leveldb orders keys and we're using BigEndian we should get the natural order at the DB level
	// automatically when using the iterator later
	newMaxHeight := indexHeight

	// Remember which heights we processed so we can read existing txs if we encounter the same height twice.
	seenHeights := make(map[uint64]struct{})

	progressLogger := newProgressLogger(15 * time.Second)
	for iter.Next() {
		progressLogger.Info("updating tx repository height index", "indexed", txIndexed, "skipped", txSkipped)

		heightBytes := iter.Value()[:wrappers.LongLen]
		height := binary.BigEndian.Uint64(heightBytes)
		if height < indexHeight {
			txSkipped++
			continue
		}

		// TODO: possibly replace with packer
		txBytes := iter.Value()[wrappers.LongLen+wrappers.IntLen:]
		txs, err := ExtractAtomicTxs(txBytes, false)
		if err != nil {
			return err
		}

		// If this height is already processed, get existing txs for that height.
		if _, exists := seenHeights[height]; exists {
			existingTxs, err := a.GetByHeight(height)
			if err != nil {
				return err
			}
			txs = append(existingTxs, txs...)
		}
		seenHeights[height] = struct{}{}

		txsBytes, err := Codec.Marshal(codecVersion, txs)
		if err != nil {
			return err
		}
		if err := a.acceptedAtomicTxByHeightDB.Put(heightBytes, txsBytes); err != nil {
			return err
		}
		// update our height record
		if height > newMaxHeight {
			newMaxHeight = height
		}

		txIndexed++
	}

	newMaxHeightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(newMaxHeightBytes, newMaxHeight)
	if err := a.acceptedAtomicTxByHeightDB.Put(maxIndexedHeightKey, newMaxHeightBytes); err != nil {
		return err
	}

	log.Info("initialized atomic tx repository", "indexHeight", indexHeight, "maxHeight", newMaxHeight, "duration", time.Since(startTime), "txIndexed", txIndexed, "txSkipped", txSkipped)
	return nil
}

func (a *atomicTxRepository) getIndexHeight() (bool, uint64, error) {
	exists, err := a.acceptedAtomicTxByHeightDB.Has(maxIndexedHeightKey)
	if err != nil {
		return false, 0, err
	}
	if !exists {
		return exists, 0, nil
	}

	indexHeightBytes, err := a.acceptedAtomicTxByHeightDB.Get(maxIndexedHeightKey)
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
	txs, err := ExtractAtomicTxs(txBytes, false)
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
	return ExtractAtomicTxs(txsBytes, true)
}

// Write updates indexes maintained on atomic txs, so they can be queried
// by txID or height. This method must be called only once per height,
// and [txs] must include all atomic txs for the block accepted at the
// corresponding height.
func (a *atomicTxRepository) Write(height uint64, txs []*Tx) error {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	for _, tx := range txs {
		txBytes, err := Codec.Marshal(codecVersion, tx)
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

	txsBytes, err := Codec.Marshal(codecVersion, txs)
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

func (a *atomicTxRepository) IterateByHeight(height uint64) database.Iterator {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
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
