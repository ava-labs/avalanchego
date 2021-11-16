package evm

import (
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	atomicTxIDDBPrefix = []byte("atomicTxDB")
)

// AtomicTxRepository defines an entity that manages storage and indexing of
// atomic transactions
type AtomicTxRepository interface {
	GetByTxID(txID ids.ID) (*Tx, uint64, error)
	ParseTxBytes(bytes []byte) (*Tx, error)
	Write(height uint64, txs []*Tx) error
	Iterate() database.Iterator
}

// atomicTxRepository is a prefixdb implementation of the AtomicTxRepository interface
type atomicTxRepository struct {
	// [acceptedAtomicTxDB] maintains an index of [txID] => [height]+[atomic tx] for all accepted atomic txs.
	acceptedAtomicTxDB database.Database
	codec              codec.Manager
}

func newAtomicTxRepository(db database.Database, codec codec.Manager) AtomicTxRepository {
	acceptedAtomicTxDB := prefixdb.New(atomicTxIDDBPrefix, db)

	return &atomicTxRepository{
		acceptedAtomicTxDB: acceptedAtomicTxDB,
		codec:              codec,
	}
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
	return nil
}

func (a *atomicTxRepository) Iterate() database.Iterator {
	return a.acceptedAtomicTxDB.NewIterator()
}
