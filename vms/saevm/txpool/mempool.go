// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/snow"
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

	ctx *snow.Context
}

func New(txs *Txs, ctx *snow.Context) *Mempool {
	return &Mempool{
		Txs: txs,
		ctx: ctx,
	}
}

func (m *Mempool) Add(rawTx *tx.Tx) error {
	if err := rawTx.Verify(context.TODO(), m.ctx); err != nil {
		return err
	}

	tx, err := NewTx(rawTx, m.ctx.AVAXAssetID)
	if err != nil {
		return err
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.txs.Get(tx.ID); ok {
		return ErrAlreadyKnown
	}

	for input := range tx.Inputs {
		if conflictID, ok := m.utxos.GetKey(input); ok {
			conflict, _ := m.txs.Get(conflictID)
			if tx.GasPrice.Cmp(&conflict.GasPrice) <= 0 {
				return errInsufficientFee
			}
		}
	}
	m.removeConflicts(tx.Inputs)

	if m.txs.Len() >= maxSize {
		_, cheap, _ := m.txs.Peek()
		if tx.GasPrice.Cmp(&cheap.GasPrice) <= 0 {
			return errInsufficientFee
		}
		m.removeConflicts(cheap.Inputs)
	}

	m.utxos.Put(tx.ID, tx.Inputs)
	m.txs.Push(tx.ID, tx)
	m.cond.Broadcast()
	return nil
}
