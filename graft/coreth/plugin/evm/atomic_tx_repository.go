package evm

import (
	"encoding/binary"
	"errors"
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
	ParseTxBytes(bytes []byte) (*Tx, error)
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

func newAtomicTxRepository(db database.Database, codec codec.Manager) AtomicTxRepository {
	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)
	acceptedAtomicTxByHeightDB := prefixdb.New(atomicHeightTxDBPrefix, db)

	return &atomicTxRepository{
		acceptedAtomicTxDB:         acceptedAtomicTxDB,
		acceptedAtomicTxByHeightDB: acceptedAtomicTxByHeightDB,
		codec:                      codec,
	}
}

func (a *atomicTxRepository) Initialize() error {
	startTime := time.Now()
	log.Info("initializing atomic tx repository")

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

	lastUpdate := time.Now()
	txIndexed, txSkipped := 0, 0
	iter := a.acceptedAtomicTxDB.NewIterator()
	defer iter.Release()
	heightIndexBatch := a.acceptedAtomicTxByHeightDB.NewBatch()

	oneBytes := make([]byte, wrappers.ShortLen)
	binary.BigEndian.PutUint16(oneBytes, 1)
	// Since leveldb orders keys and since we're using BigEndian we should get the natural order at the DB level
	// automatically when using the iterator later
	newMaxHeight := indexHeight
	newMaxHeightBytes := indexHeightBytes
	for iter.Next() {
		heightBytes := iter.Value()[:wrappers.LongLen]
		height := binary.BigEndian.Uint64(heightBytes)

		if exists && height < indexHeight {
			txSkipped++
			goto logProgress
		}

		// TODO handling for multiple tx if this goes in after multiple atomic tx PR

		// we're initialising on this node for the first time or this is a new height we're seeing
		if err = heightIndexBatch.Put(heightBytes, append(oneBytes, iter.Value()[wrappers.LongLen:]...)); err != nil {
			return err
		}

		// update our height record
		if height > newMaxHeight {
			newMaxHeight = height
			newMaxHeightBytes = heightBytes
		}

		txIndexed++

	logProgress:
		if time.Since(lastUpdate) > 15*time.Second {
			log.Info("updating tx repository height index", "indexed", txIndexed, "skipped", txSkipped)
			lastUpdate = time.Now()
		}
	}

	if err = heightIndexBatch.Put(maxIndexedHeightKey, newMaxHeightBytes); err != nil {
		return err
	}

	if err = heightIndexBatch.Write(); err != nil {
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

	packer := wrappers.Packer{Bytes: indexedTxBytes}
	height := packer.UnpackLong()
	txBytes := packer.UnpackBytes()
	tx, err := a.ParseTxBytes(txBytes)
	if err != nil {
		return nil, 0, err
	}

	return tx, height, nil
}

func (a *atomicTxRepository) GetByHeight(height uint64) ([]*Tx, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	b, err := a.acceptedAtomicTxByHeightDB.Get(heightBytes)
	if err != nil {
		return nil, err
	}

	if len(b) == 0 {
		return nil, errors.New("not found")
	}

	packer := wrappers.Packer{Bytes: b}
	txLen := packer.UnpackShort()
	txs := make([]*Tx, txLen)
	for i := uint16(0); i < txLen; i++ {
		txBytes := packer.UnpackBytes()
		tx, err := a.ParseTxBytes(txBytes)
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

	// packer format [tx count] [[tx1ByteLen][tx1Bytes]] [[tx2ByteLen][tx2Bytes]] ...
	packerLen := wrappers.ShortLen + len(txs) + (len(txs) * wrappers.IntLen)
	txsPacker := wrappers.Packer{Bytes: make([]byte, packerLen), MaxSize: 1024 * 1024}
	txsPacker.PackShort(uint16(len(txs)))

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
		txsPacker.PackBytes(txBytes)
	}

	if err := a.acceptedAtomicTxByHeightDB.Put(heightBytes, txsPacker.Bytes); err != nil {
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
