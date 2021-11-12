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

const (
	txIDLen              = 32
	atomicTxsMaxLenBytes = 1024 * 1024
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
	ParseTxBytes(bytes []byte) (*Tx, error)
	Write(height uint64, txs []*Tx) error
	IterateByHeight(startHeight uint64) database.Iterator
}

type atomicTxRepository struct {
	// [acceptedAtomicTxDB] maintains an index of [txID] => [atomic tx] for all accepted atomic txs.
	acceptedAtomicTxDB database.Database
	// [acceptedHeightAtomicTxDB] maintains an index of block height => [atomic tx count][atomic tx0][atomic tx1]...
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
// txID => [height][txBytes] to height => [txCount][txBytes0][txBytes1]...
func (a *atomicTxRepository) Initialize() error {
	startTime := time.Now()

	// initialise atomicHeightTxDB if not done already
	heightTxDBInitialized, err := a.acceptedHeightAtomicTxDB.Has(heightAtomicTxDBInitializedKey)
	if err != nil {
		return err
	}

	if heightTxDBInitialized {
		log.Info("acceptedHeightAtomicTxDB already initialized")
		return nil
	}
	log.Info("initializing acceptedHeightAtomicTxDB", "startTime", startTime)

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

		// read heightBytes and txBytes from iter.Value()
		unpacker := wrappers.Packer{Bytes: iter.Value()}
		// heightBytes will be directly used as key in [acceptedHeightAtomicTxDB] index
		heightBytes := unpacker.UnpackFixedBytes(wrappers.LongLen)
		txBytes := unpacker.UnpackBytes()

		// write [heightBytes] => [tx count] + ([tx bytes] ...),
		// NOTE: code here assumes only one atomic tx / height
		// This code should be modified if we need to rebuild the height
		// index and there may be multiple atomic tx per block height.
		packer := wrappers.Packer{Bytes: make([]byte, wrappers.ShortLen+wrappers.LongLen+len(txBytes))}
		packer.PackShort(1)
		packer.PackBytes(txBytes)

		if err := batch.Put(heightBytes, packer.Bytes); err != nil {
			return fmt.Errorf("error saving tx bytes to  during init: %w", err)
		}

		entries++
		logger.Info("entries indexed to acceptedHeightAtomicTxDB", "entries", entries)
	}

	if err := batch.Put(heightAtomicTxDBInitializedKey, nil); err != nil {
		return err
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("error writing acceptedHeightAtomicTxDB batch: %w", err)
	}

	log.Info("finished initializing acceptedHeightAtomicTxDB", "time", time.Since(startTime))
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

	packer := wrappers.Packer{Bytes: indexedTxBytes}
	height := packer.UnpackLong()
	txBytes := packer.UnpackBytes()
	tx, err := a.ParseTxBytes(txBytes)
	if err != nil {
		return nil, 0, err
	}

	return tx, height, nil
}

// GetByHeight queries [acceptedHeightAtomicTxDB] for atomic txs that were accepted
// on the block specified by [height] and parses them into [*Tx] objects, returning
// them along with an optional error.
func (a *atomicTxRepository) GetByHeight(height uint64) ([]*Tx, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	txsBytes, err := a.acceptedHeightAtomicTxDB.Get(heightBytes)
	if err != nil {
		return nil, err
	}

	unpacker := wrappers.Packer{Bytes: txsBytes}
	txCount := unpacker.UnpackShort()
	txs := make([]*Tx, txCount)
	for i := 0; i < int(txCount); i++ {
		tx, err := a.ParseTxBytes(unpacker.UnpackBytes())
		if err != nil {
			return nil, err
		}
		txs[i] = tx
	}
	return txs, nil
}

// ParseTxBytes parses [bytes] to a [*Tx] object using the codec provided to the
// atomicTxRepository.
func (a *atomicTxRepository) ParseTxBytes(bytes []byte) (*Tx, error) {
	if len(bytes) < wrappers.LongLen {
		return nil, fmt.Errorf("bytes too short to parse atomic tx: %d", len(bytes))
	}

	tx := &Tx{}
	if _, err := a.codec.Unmarshal(bytes, tx); err != nil {
		return nil, fmt.Errorf("problem parsing atomic tx from db: %w", err)
	}
	if err := tx.Sign(a.codec, nil); err != nil {
		return nil, fmt.Errorf("problem initializing atomic tx from db: %w", err)
	}

	return tx, nil
}

// Write updates [txID] => [height][txBytes] and [height] => [txCount][txBytes0][txBytes1]...
// records for all atomic txs specified in [txs], and returns an optional error.
func (a *atomicTxRepository) Write(height uint64, txs []*Tx) error {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	// 4 bytes for num representing number of transactions
	packer := wrappers.Packer{Bytes: make([]byte, wrappers.ShortLen), MaxSize: atomicTxsMaxLenBytes}
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

// IterateByHeight returns a new iterator on acceptedHeightAtomicTxDB starting at
// [startHeight]. This iterator can be used to fetch all atomic transactions accepted on
// blocks >= [startHeight].
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
