package evm

import (
	"bytes"
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

const txIDLen = 32

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
	ParseTxsBytes(bytes []byte) ([]*Tx, error)
	Write(height uint64, txs []*Tx) error
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
	startTime := time.Now()

	// initialise atomicHeightTxDB if not done already
	heightTxDBInitialized, err := a.acceptedHeightAtomicTxDB.Has(heightAtomicTxDBInitializedKey)
	if err != nil {
		return err
	}

	if heightTxDBInitialized {
		log.Info("skipping atomic tx by height index init")
		return nil
	}

	log.Info("initializing atomic tx by height index", "startTime", startTime)

	iter := a.acceptedAtomicTxDB.NewIterator()
	defer iter.Release()
	batch := a.acceptedHeightAtomicTxDB.NewBatch()

	logger := NewProgressLogger(10 * time.Second)
	entries := uint32(0)
	for iter.Next() {
		// if there was an error during iteration, return now
		if err := iter.Error(); err != nil {
			return err
		}

		if len(iter.Key()) != txIDLen || bytes.Equal(iter.Key(), heightAtomicTxDBInitializedKey) {
			continue
		}

		tx, height, err := a.ParseTxBytes(iter.Value())
		if err != nil {
			return err
		}

		// map [height] => [tx count] + ( [tx bytes] ... )
		// NOTE: this assumes there is only one atomic tx / height.
		// This code should be modified if we need to rebuild the height
		// index and there may be multiple atomic tx per block height.
		heightBytes := make([]byte, wrappers.LongLen)
		binary.BigEndian.PutUint64(heightBytes, height)

		packer := wrappers.Packer{Bytes: make([]byte, wrappers.ShortLen), MaxSize: 1024 * 1024}
		packer.PackShort(1)

		txBytes, err := a.codec.Marshal(codecVersion, tx)
		if err != nil {
			return err
		}

		packer.PackBytes(txBytes)
		if packer.Errored() {
			return packer.Err
		}

		if err = batch.Put(heightBytes, packer.Bytes); err != nil {
			return fmt.Errorf("error saving tx bytes to atomic tx by height index during init: %w", err)
		}

		entries++
		logger.Info("entries indexed to atomic tx by height index", "entries", entries)
	}

	if err = batch.Put(heightAtomicTxDBInitializedKey, nil); err != nil {
		return err
	}

	if err = batch.Write(); err != nil {
		return fmt.Errorf("error writing atomic tx by height index batch: %w", err)
	}

	log.Info("finished initializing atomic tx by height index", "time", time.Since(startTime))
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
	if len(bytes) == 0 || len(bytes) < wrappers.LongLen {
		return nil, 0, nil
	}

	packer := wrappers.Packer{Bytes: bytes}
	height := packer.UnpackLong()
	txBytes := packer.UnpackBytes()

	if len(txBytes) == 0 {
		return nil, 0, nil
	}

	tx := &Tx{}
	if _, err := a.codec.Unmarshal(txBytes, tx); err != nil {
		return nil, 0, fmt.Errorf("problem parsing atomic tx from db: %w", err)
	}
	if err := tx.Sign(a.codec, nil); err != nil {
		return nil, 0, fmt.Errorf("problem initializing atomic tx from db: %w", err)
	}

	return tx, height, nil
}

func (a *atomicTxRepository) ParseTxsBytes(bytes []byte) ([]*Tx, error) {
	if len(bytes) == 0 || len(bytes) < wrappers.ShortLen {
		return nil, nil
	}

	packer := wrappers.Packer{Bytes: bytes}
	count := packer.UnpackShort()
	txs := make([]*Tx, count)

	if count == 0 {
		return nil, nil
	}

	for i := uint16(0); i < count; i++ {
		txBytes := packer.UnpackBytes()
		tx := &Tx{}
		if _, err := a.codec.Unmarshal(txBytes, tx); err != nil {
			return nil, fmt.Errorf("problem parsing atomic txs from db, txBytesLen=%d, err=%w", len(txBytes), err)
		}
		if err := tx.Sign(a.codec, nil); err != nil {
			return nil, fmt.Errorf("problem initializing atomic txs from db, txBytesLen=%d, err=%w", len(txBytes), err)
		}
		txs[i] = tx
	}
	return txs, nil
}

func (a *atomicTxRepository) GetByHeight(height uint64) ([]*Tx, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	txsBytes, err := a.acceptedHeightAtomicTxDB.Get(heightBytes)
	if err != nil {
		return nil, err
	}

	return a.ParseTxsBytes(txsBytes)
}

func (a *atomicTxRepository) Write(height uint64, txs []*Tx) error {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	// 4 bytes for num representing number of transactions
	totalPackerLen := wrappers.ShortLen
	for _, tx := range txs {
		// for each tx
		// 4 bytes for number of tx byte length + the number of bytes for tx bytes itself
		totalPackerLen += wrappers.IntLen + len(tx.Bytes())
	}

	packer := wrappers.Packer{Bytes: make([]byte, totalPackerLen), MaxSize: 1024 * 1024}
	packer.PackShort(uint16(len(txs)))

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
		packer.PackBytes(txBytes)
	}
	// map height => txs bytes
	return a.acceptedHeightAtomicTxDB.Put(heightBytes, packer.Bytes)
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
