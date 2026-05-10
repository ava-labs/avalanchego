// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"iter"

	// Imported for [state.AtomicRepository] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// Tx index database layout — these prefixes must remain byte-compatible
// with entries written by [state.AtomicRepository] so the chain can read
// entries produced by older nodes:
//
//	atomicTxDB     / [txID(32)]       -> [height(8 BE)] [len(4 BE)] [tx_bytes]
//	atomicHeightTxDB / [height(8 BE)] -> codec.Marshal(0, []*tx.Tx)
var (
	txIDToTxPrefix    = prefixdb.MakePrefix([]byte("atomicTxDB"))
	heightToTxsPrefix = prefixdb.MakePrefix([]byte("atomicHeightTxDB"))
)

// txsAndHeight pairs a slice of accepted transactions with the block height
// they were accepted at.
type txsAndHeight struct {
	txs    []*tx.Tx
	height uint64
}

// writeSortedTxs indexes txs by ID and by height into batch. Txs must be
// sorted by ID to match the order [state.AtomicRepository.write] used; the
// height index must contain the same bytes here.
//
// Mirrors [state.AtomicRepository.write].
func writeSortedTxs(batch database.KeyValueWriter, height uint64, sorted []*tx.Tx) error {
	if len(sorted) == 0 {
		return nil
	}

	for _, tx := range sorted {
		if err := writeTxByID(batch, height, tx); err != nil {
			return err
		}
	}
	return writeTxsByHeight(batch, height, sorted)
}

// readTxByID returns the tx with the given ID along with the block height
// it was accepted at.
//
// Mirrors [state.AtomicRepository.GetByTxID].
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
// starting at startHeight in ascending height order. Heights with no txs
// are skipped (the caller is expected to know that empty heights don't
// change the trie).
//
// Mirrors [state.AtomicRepository.IterateByHeight].
func iterateTxsFromHeight(db database.Iteratee, startHeight uint64) iter.Seq2[txsAndHeight, error] {
	return func(yield func(txsAndHeight, error) bool) {
		it := db.NewIteratorWithStartAndPrefix(
			prefixdb.PrefixKey(heightToTxsPrefix, database.PackUInt64(startHeight)),
			heightToTxsPrefix,
		)
		defer it.Release()

		for it.Next() {
			height, err := database.ParseUInt64(it.Key()[len(heightToTxsPrefix):])
			if err != nil {
				yield(txsAndHeight{}, err)
				return
			}

			txs, err := tx.ParseSlice(it.Value())
			if err != nil {
				yield(txsAndHeight{}, err)
				return
			}

			if !yield(txsAndHeight{txs: txs, height: height}, nil) {
				return
			}
		}

		if err := it.Error(); err != nil {
			yield(txsAndHeight{}, err)
		}
	}
}
