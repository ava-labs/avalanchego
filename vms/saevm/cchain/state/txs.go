// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// This file mirrors graft/coreth/plugin/evm/atomic/state/atomic_repository.go.
// References to the old code are temporary; they will be removed once that
// package is deleted.

package state

import (
	"fmt"
	"iter"
	"slices"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// Tx index database layout (must remain compatible with the old coreth code):
//
//	atomicTxDB     / [txID(32)]       -> [height(8 BE)] [len(4 BE)] [tx_bytes]
//	atomicHeightTxDB / [height(8 BE)] -> codec.Marshal(0, []*tx.Tx)
var (
	txIDToTxPrefix    = prefixdb.MakePrefix([]byte("atomicTxDB"))
	heightToTxsPrefix = prefixdb.MakePrefix([]byte("atomicHeightTxDB"))
)

// TxsAndHeight pairs a slice of accepted transactions with the block height
// they were accepted at. Returned by [State.IterateTxsFromHeight].
type TxsAndHeight struct {
	Txs    []*tx.Tx
	Height uint64
}

// writeTxs indexes txs by ID and by height into batch. Txs are sorted by ID
// before writing to match the order produced by the legacy txID-only
// migration that pre-AP5 nodes ran.
//
// Mirrors AtomicRepository.write (atomic_repository.go:256).
func writeTxs(batch database.KeyValueWriter, height uint64, txs []*tx.Tx) error {
	if len(txs) == 0 {
		return nil
	}

	txs = slices.Clone(txs)
	slices.SortFunc(txs, func(a, b *tx.Tx) int {
		return a.ID().Compare(b.ID())
	})

	for _, tx := range txs {
		if err := writeTxByID(batch, height, tx); err != nil {
			return err
		}
	}
	return writeTxsByHeight(batch, height, txs)
}

// readTxByID returns the tx with the given ID along with the block height it
// was accepted at.
//
// Mirrors AtomicRepository.GetByTxID (atomic_repository.go:199).
func readTxByID(db database.KeyValueReader, txID ids.ID) (*tx.Tx, uint64, error) {
	b, err := db.Get(prefixdb.PrefixKey(txIDToTxPrefix, txID[:]))
	if err != nil {
		return nil, 0, fmt.Errorf("reading tx: %w", err)
	}

	p := wrappers.Packer{Bytes: b}
	height := p.UnpackLong()
	txBytes := p.UnpackBytes()
	if p.Errored() {
		return nil, 0, fmt.Errorf("unpacking tx: %w", p.Err)
	}

	parsed, err := tx.Parse(txBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing tx: %w", err)
	}
	return parsed, height, nil
}

func writeTxByID(db database.KeyValueWriter, height uint64, tx *tx.Tx) error {
	txBytes, err := tx.Bytes()
	if err != nil {
		return err
	}

	p := wrappers.Packer{
		Bytes: make([]byte, wrappers.LongLen+wrappers.IntLen+len(txBytes)),
	}
	p.PackLong(height)
	p.PackBytes(txBytes)

	txID := tx.ID()
	return db.Put(prefixdb.PrefixKey(txIDToTxPrefix, txID[:]), p.Bytes)
}

func writeTxsByHeight(db database.KeyValueWriter, height uint64, txs []*tx.Tx) error {
	txsBytes, err := tx.MarshalSlice(txs)
	if err != nil {
		return err
	}
	return db.Put(prefixdb.PrefixKey(heightToTxsPrefix, database.PackUInt64(height)), txsBytes)
}

// iterateTxsFromHeight returns an iterator yielding (txs, height) entries
// starting at startHeight in ascending height order.
//
// Mirrors AtomicRepository.IterateByHeight (atomic_repository.go:354).
func iterateTxsFromHeight(db database.Iteratee, startHeight uint64) iter.Seq2[TxsAndHeight, error] {
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

			if !yield(TxsAndHeight{Txs: txs, Height: height}, nil) {
				return
			}
		}

		if err := it.Error(); err != nil {
			yield(TxsAndHeight{}, err)
		}
	}
}
