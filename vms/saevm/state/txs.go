// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"iter"
	"slices"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
)

// WriteTxs indexes the on atomic txs, so they can be queried by txID or height.
func WriteTxs(db database.Database, height uint64, txs []*tx.Tx) error {
	return writeTxs(db, height, txs, false)
}

// WriteBonusTxs is similar to [WriteTxs], except txIDs mappings are not
// overwritten if they already exists.
func WriteBonusTxs(db database.Database, height uint64, txs []*tx.Tx) error {
	return writeTxs(db, height, txs, true)
}

func writeTxs(db database.Database, height uint64, txs []*tx.Tx, bonus bool) error {
	// Skip adding an entry to the height index if txs is empty.
	if len(txs) == 0 {
		return nil
	}

	// txs are stored in order of txID to ensure consistency with txs
	// indexed from the txID index during a prior database migration.
	txs = slices.Clone(txs)
	utils.Sort(txs)

	batch := db.NewBatch()
	for _, tx := range txs {
		if bonus {
			txID, err := tx.ID()
			if err != nil {
				return err
			}

			has, err := hasTxByID(db, txID)
			if err != nil {
				return err
			}
			if has {
				continue // avoid overwriting when bonus
			}
		}
		if err := writeTxByID(batch, height, tx); err != nil {
			return err
		}
	}

	if err := writeTxsByHeight(batch, height, txs); err != nil {
		return err
	}
	return batch.Write()
}

var txIDToTxPrefix = prefixdb.MakePrefix([]byte("atomicTxDB")) // txID -> height + tx

func ReadTxByID(db database.KeyValueReader, txID ids.ID) (*tx.Tx, uint64, error) {
	b, err := db.Get(prefixdb.PrefixKey(txIDToTxPrefix, txID[:]))
	if err != nil {
		return nil, 0, err
	}

	p := wrappers.Packer{Bytes: b}
	height := p.UnpackLong()
	txBytes := p.UnpackBytes()
	if p.Errored() {
		return nil, 0, fmt.Errorf("error unpacking atomic tx: %w", p.Err)
	}

	tx, err := tx.Parse(txBytes)
	if err != nil {
		return nil, 0, err
	}
	return tx, height, nil
}

func hasTxByID(db database.KeyValueReader, txID ids.ID) (bool, error) {
	return db.Has(prefixdb.PrefixKey(txIDToTxPrefix, txID[:]))
}

func writeTxByID(db database.KeyValueWriter, height uint64, tx *tx.Tx) error {
	txID, err := tx.ID()
	if err != nil {
		return err
	}

	txBytes, err := tx.Bytes()
	if err != nil {
		return err
	}

	p := wrappers.Packer{
		Bytes: make([]byte, wrappers.LongLen+wrappers.IntLen+len(txBytes)),
	}
	p.PackLong(height)
	p.PackBytes(txBytes)
	return db.Put(prefixdb.PrefixKey(txIDToTxPrefix, txID[:]), p.Bytes)
}

var heightToTxsPrefix = prefixdb.MakePrefix([]byte("atomicHeightTxDB")) // height -> []txs

type TxsAndHeight struct {
	Txs    []*tx.Tx
	Height uint64
}

func IterateTxsByHeight(db database.Iteratee, startHeight uint64) iter.Seq2[TxsAndHeight, error] {
	return func(yield func(TxsAndHeight, error) bool) {
		it := db.NewIteratorWithStartAndPrefix(
			prefixdb.PrefixKey(heightToTxsPrefix, database.PackUInt64(startHeight)),
			heightToTxsPrefix,
		)
		defer it.Release()

		for it.Next() {
			height, err := database.ParseUInt64(it.Key()[len(heightToTxsPrefix):])
			if err != nil {
				yield(TxsAndHeight{}, err)
				return
			}

			txs, err := tx.ParseSlice(it.Value())
			if err != nil {
				yield(TxsAndHeight{}, err)
				return
			}

			if !yield(TxsAndHeight{txs, height}, nil) {
				return
			}
		}

		if err := it.Error(); err != nil {
			yield(TxsAndHeight{}, err)
		}
	}
}

func writeTxsByHeight(db database.KeyValueWriter, height uint64, txs []*tx.Tx) error {
	txsBytes, err := tx.MarshalSlice(txs)
	if err != nil {
		return err
	}

	heightBytes := database.PackUInt64(height)
	return db.Put(prefixdb.PrefixKey(heightToTxsPrefix, heightBytes), txsBytes)
}
