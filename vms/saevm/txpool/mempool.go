// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
)

const maxSize = 4096

var (
	ErrAlreadyKnown    = errors.New("transaction already in mempool")
	errInsufficientFee = errors.New("insufficient fee")
)

// Mempool is a simple mempool for atomic transactions
type Mempool struct {
	*Txs

	avaxAssetID ids.ID
}

func New(txs *Txs, avaxAssetID ids.ID) *Mempool {
	return &Mempool{
		Txs:         txs,
		avaxAssetID: avaxAssetID,
	}
}

func (m *Mempool) Add(tx *tx.Tx) error {
	gasPrice, err := tx.GasPrice(m.avaxAssetID)
	if err != nil {
		return err
	}

	inputs := tx.InputUTXOs()

	// TODO: Verify tx against the atomic state

	txID, err := tx.ID()
	if err != nil {
		return err
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.txs.Get(txID); ok {
		return ErrAlreadyKnown
	}

	for input := range inputs {
		if conflictID, ok := m.utxos.GetKey(input); ok {
			conflict, _ := m.txs.Get(conflictID)
			if gasPrice.Cmp(&conflict.GasPrice) <= 0 {
				return errInsufficientFee
			}
		}
	}
	m.removeConflicts(inputs)

	if m.txs.Len() >= maxSize {
		_, cheap, _ := m.txs.Peek()
		if gasPrice.Cmp(&cheap.GasPrice) <= 0 {
			return errInsufficientFee
		}
		m.removeConflicts(cheap.Inputs)
	}

	m.utxos.Put(txID, inputs)
	m.txs.Push(txID, &Transaction{
		ID:       txID,
		Tx:       tx,
		Inputs:   inputs,
		GasPrice: gasPrice,
	})
	return nil
}
