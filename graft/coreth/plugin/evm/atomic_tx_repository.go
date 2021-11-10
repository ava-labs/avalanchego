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
	GetByHeight(height uint64) (*Tx, error)
	Write(height uint64, tx *Tx) error
	IterateByHeight() database.Iterator
}

type atomicTxRepository struct {
	// [acceptedAtomicTxDB] maintains an index of accepted atomic txs.
	acceptedAtomicTxDB database.Database
	// [acceptedHeightAtomicTxDB] maintains an index of block height => [atomic tx].
	acceptedHeightAtomicTxDB database.Database
	codec                    codec.Manager
}

func newAtomicTxRepository(db *versiondb.Database, codec codec.Manager) AtomicTxRepository {
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
	// initialise the atomicHeightTxDB if not done already
	heightTxDBInitialized, err := a.acceptedHeightAtomicTxDB.Has(heightAtomicTxDBInitializedKey)
	if err != nil {
		return err
	}

	if !heightTxDBInitialized {
		log.Info("initializing acceptedHeightAtomicTxDB")
		startTime := time.Now()
		iter := a.acceptedAtomicTxDB.NewIterator()
		batch := a.acceptedHeightAtomicTxDB.NewBatch()
		lastUpdate := time.Now()
		entries := uint(0)
		for iter.Next() {
			packer := wrappers.Packer{Bytes: iter.Value()}
			heightBytes := packer.UnpackFixedBytes(wrappers.LongLen)
			txBytes := packer.UnpackBytes()

			if err = batch.Put(heightBytes, txBytes); err != nil {
				return fmt.Errorf("error saving tx bytes to acceptedHeightAtomicTxDB during init: %w", err)
			}

			entries++

			if time.Since(lastUpdate) > 10*time.Second {
				log.Info("entries copied to acceptedHeightAtomicTxDB", "entries", entries)
				lastUpdate = time.Now()
			}
		}

		if err = batch.Put(heightAtomicTxDBInitializedKey, nil); err != nil {
			return err
		}

		if err = batch.Write(); err != nil {
			return fmt.Errorf("error writing acceptedHeightAtomicTxDB batch: %w", err)
		}

		log.Info("finished initializing acceptedHeightAtomicTxDB", "time", time.Since(startTime))
	} else {
		log.Info("skipping acceptedHeightAtomicTxDB init")
	}

	return nil
}

func (a *atomicTxRepository) GetByTxID(txID ids.ID) (*Tx, uint64, error) {
	indexedTxBytes, err := a.acceptedAtomicTxDB.Get(txID[:])
	if err != nil {
		return nil, 0, err
	}

	packer := wrappers.Packer{Bytes: indexedTxBytes}
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

func (a *atomicTxRepository) GetByHeight(height uint64) (*Tx, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	indexedTxBytes, err := a.acceptedHeightAtomicTxDB.Get(heightBytes)
	if err != nil {
		return nil, err
	}

	tx := &Tx{}
	if _, err := a.codec.Unmarshal(indexedTxBytes, tx); err != nil {
		return nil, fmt.Errorf("problem parsing atomic transaction from db at height %d: %w", height, err)
	}
	if err := tx.Sign(a.codec, nil); err != nil {
		return nil, fmt.Errorf("problem initializing atomic transaction from db at height %d: %w", height, err)
	}

	return tx, nil
}

func (a *atomicTxRepository) Write(height uint64, tx *Tx) error {
	txBytes := tx.Bytes()

	// map txID => [height]+[tx bytes]
	// Height is 8 bytes. txBytes len is 4 bytes and then the txBytes itself is len(txBytes)
	heightTxPacker := wrappers.Packer{Bytes: make([]byte, 12+len(txBytes))}
	heightTxPacker.PackLong(height)
	heightTxPacker.PackBytes(txBytes)
	txID := tx.ID()

	if err := a.acceptedAtomicTxDB.Put(txID[:], heightTxPacker.Bytes); err != nil {
		return err
	}

	// map height => tx bytes
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	return a.acceptedHeightAtomicTxDB.Put(heightBytes, txBytes)
}

func (a *atomicTxRepository) IterateByHeight() database.Iterator {
	return a.acceptedHeightAtomicTxDB.NewIterator()
}
