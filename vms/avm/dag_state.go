// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm/states"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ states.Chain = (*dagState)(nil)

type dagState struct {
	states.Chain
	vm *VM
}

func (s *dagState) GetUTXOFromID(utxoID *avax.UTXOID) (*avax.UTXO, error) {
	inputID := utxoID.InputID()
	utxo, err := s.GetUTXO(inputID)
	if err == nil {
		// If the UTXO exists in the base state, then we can immediately return
		// it.
		return utxo, nil
	}
	if err != database.ErrNotFound {
		s.vm.ctx.Log.Error("fetching UTXO returned unexpected error",
			zap.Stringer("txID", utxoID.TxID),
			zap.Uint32("index", utxoID.OutputIndex),
			zap.Stringer("utxoID", inputID),
			zap.Error(err),
		)
		return nil, err
	}

	// The UTXO doesn't exist in the base state, so we need to check if the UTXO
	// could exist from a currently processing tx.
	inputTxID, inputIndex := utxoID.InputSource()
	parent := UniqueTx{
		vm:   s.vm,
		txID: inputTxID,
	}

	// If the parent doesn't exist or is otherwise invalid, then this UTXO isn't
	// available.
	if err := parent.verifyWithoutCacheWrites(); err != nil {
		return nil, database.ErrNotFound
	}

	// If the parent was accepted, the UTXO should have been in the base state.
	// This means the UTXO was already consumed by a conflicting tx.
	if status := parent.Status(); status.Decided() {
		return nil, database.ErrNotFound
	}

	parentUTXOs := parent.UTXOs()

	// At this point we have only verified the TxID portion of [utxoID] as being
	// potentially valid. It is still possible that a user specified an invalid
	// index. So, we must bounds check the parents UTXOs.
	//
	// Invariant: len(parentUTXOs) <= MaxInt32. This guarantees that casting
	// inputIndex to an int, even on 32-bit architectures, will not overflow.
	if uint32(len(parentUTXOs)) <= inputIndex {
		return nil, database.ErrNotFound
	}
	return parentUTXOs[int(inputIndex)], nil
}

func (s *dagState) GetTx(txID ids.ID) (*txs.Tx, error) {
	tx := &UniqueTx{
		vm:   s.vm,
		txID: txID,
	}
	if status := tx.Status(); !status.Fetched() {
		return nil, database.ErrNotFound
	}
	return tx.Tx, nil
}
