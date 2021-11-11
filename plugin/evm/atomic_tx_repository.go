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
	heightAtomicTxDBPrefix         = []byte("heightAtomicTxDB")
	heightAtomicTxDBInitializedKey = []byte("initialized")
	atomicTxIDDBPrefix             = []byte("atomicTxDB")
)

type AtomicTxRepository interface {
	Initialize() error
	GetByTxID(txID ids.ID) (*Tx, uint64, error)
	GetByHeight(height uint64) ([]*Tx, error)
	ParseTxBytes(bytes []byte) (*Tx, uint64, error)
	Write(height uint64, txs *Tx) error
	IterateByHeight(startHeight uint64) database.Iterator
}

type atomicTxRepository struct {
	// [acceptedAtomicTxDB] maintains an index of [txID] => [atomic tx] for all accepted atomic txs.
	acceptedAtomicTxDB database.Database
	// [acceptedHeightAtomicTxDB] maintains an index of block height => [atomic tx].
	acceptedHeightAtomicTxDB database.Database
	codec                    codec.Manager
}

func newAtomicTxRepository(db database.Database, codec codec.Manager) AtomicTxRepository {
	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	acceptedHeightAtomicTxDB := prefixdb.New(heightAtomicTxDBPrefix, db)

	return &atomicTxRepository{
		acceptedHeightAtomicTxDB: acceptedHeightAtomicTxDB,
		acceptedAtomicTxDB:       acceptedAtomicTxDB,
		codec:                    codec,
	}
}

// Initialize initializes the atomicTxRepository by re-mapping entries from
// txID => [height][txBytes] to height => [txBytes]
func (a *atomicTxRepository) Initialize() error {
	// initialise atomicHeightTxDB if not done already
	heightTxDBInitialized, err := a.acceptedHeightAtomicTxDB.Has(heightAtomicTxDBInitializedKey)
	if err != nil {
		return err
	}
	if heightTxDBInitialized {
		log.Info("skipping acceptedHeightAtomicTxDB init")
		return nil
	}

	startTime := time.Now()
	log.Info("initializing acceptedHeightAtomicTxDB", "startTime", startTime)
	iter := a.acceptedAtomicTxDB.NewIterator()
	batch := a.acceptedHeightAtomicTxDB.NewBatch()

	logger := NewProgressLogger(10 * time.Second)
	entries := uint(0)
	for iter.Next() {
		if err := iter.Error(); err != nil {
			return err
		}
		tx, height, err := a.ParseTxBytes(iter.Value())
		if err != nil {
			return err
		}
		txID := tx.ID()

		// map [height] + [txID] => tx bytes
		keyPacker := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen+len(txID))}
		keyPacker.PackLong(height)
		keyPacker.PackFixedBytes(txID[:])

		if err = batch.Put(keyPacker.Bytes, iter.Value()); err != nil {
			return fmt.Errorf("error saving tx bytes to acceptedHeightAtomicTxDB during init: %w", err)
		}

		entries++
		logger.Info("entries indexed to acceptedHeightAtomicTxDB", "entries", entries)
	}

	if err = batch.Put(heightAtomicTxDBInitializedKey, nil); err != nil {
		return err
	}

	if err = batch.Write(); err != nil {
		return fmt.Errorf("error writing acceptedHeightAtomicTxDB batch: %w", err)
	}

	log.Info("finished initializing acceptedHeightAtomicTxDB", "time", time.Since(startTime))
	return nil
}

func (a *atomicTxRepository) GetByTxID(txID ids.ID) (*Tx, uint64, error) {
	indexedTxBytes, err := a.acceptedAtomicTxDB.Get(txID[:])
	if err != nil {
		return nil, 0, err
	}
	return a.ParseTxBytes(indexedTxBytes)
}

func (a *atomicTxRepository) ParseTxBytes(bytes []byte) (*Tx, uint64, error) {
	packer := wrappers.Packer{Bytes: bytes}
	height := packer.UnpackLong()
	txBytes := packer.UnpackBytes()

	tx := &Tx{}
	if _, err := a.codec.Unmarshal(txBytes, tx); err != nil {
		return nil, 0, fmt.Errorf("problem parsing atomic transaction from db: %w", err)
	}
	if err := tx.Sign(a.codec, nil); err != nil {
		return nil, 0, fmt.Errorf("problem initializing atomic transaction from db: %w", err)
	}

	return tx, height, nil

}

func (a *atomicTxRepository) GetByHeight(height uint64) ([]*Tx, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	txs := make([]*Tx, 0)
	it := a.acceptedHeightAtomicTxDB.NewIteratorWithPrefix(heightBytes)
	for it.Next() {
		if err := it.Error(); err != nil {
			return nil, err
		}
		tx := &Tx{}
		if _, err := a.codec.Unmarshal(it.Value(), tx); err != nil {
			return nil, fmt.Errorf("problem parsing atomic transaction from db at height %d: %w", height, err)
		}
		for _, tx := range txs {
			if err := tx.Sign(a.codec, nil); err != nil {
				return nil, fmt.Errorf("problem initializing atomic transaction (txId %v) from db at height %d: %w", tx.ID(), height, err)
			}
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func (a *atomicTxRepository) Write(height uint64, tx *Tx) error {
	txBytes := tx.Bytes()

	// map txID => [height]+[tx bytes]
	heightTxPacker := wrappers.Packer{Bytes: make([]byte, 12+len(txBytes))}
	heightTxPacker.PackLong(height)
	heightTxPacker.PackBytes(txBytes)
	txID := tx.ID()

	txIdBatch := a.acceptedAtomicTxDB.NewBatch()
	if err := txIdBatch.Put(txID[:], heightTxPacker.Bytes); err != nil {
		return err
	}

	// map [height] + [txID] => tx bytes
	keyPacker := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen+len(txID))}
	keyPacker.PackLong(height)
	keyPacker.PackFixedBytes(txID[:])
	return a.acceptedHeightAtomicTxDB.Put(keyPacker.Bytes, txBytes)
}

func (a *atomicTxRepository) IterateByHeight(startHeight uint64) database.Iterator {
	startHeightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(startHeightBytes, startHeight)
	return a.acceptedHeightAtomicTxDB.NewIteratorWithStart(startHeightBytes)
}

type progressLogger struct {
	lastUpdate     time.Time
	updateInterval time.Duration
}

func NewProgressLogger(updateInterval time.Duration) *progressLogger {
	return &progressLogger{
		lastUpdate:     time.Now(),
		updateInterval: updateInterval,
	}
}

func (pl *progressLogger) Info(msg string, vals ...interface{}) {
	if time.Since(pl.lastUpdate) >= pl.updateInterval {
		log.Info(msg, vals...)
		pl.lastUpdate = time.Now()
	}
}
