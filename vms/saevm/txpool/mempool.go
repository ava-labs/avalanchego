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

func (m *Mempool) Add(tx *tx.Tx) error {
	if err := tx.Verify(context.TODO(), m.ctx); err != nil {
		return err
	}
	op, err := tx.AsOp(m.ctx.AVAXAssetID)
	if err != nil {
		return err
	}
	inputs := tx.InputUTXOs()

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.txs.Get(op.ID); ok {
		return ErrAlreadyKnown
	}

	for input := range inputs {
		if conflictID, ok := m.utxos.GetKey(input); ok {
			conflict, _ := m.txs.Get(conflictID)
			if op.GasFeeCap.Cmp(&conflict.GasPrice) <= 0 {
				return errInsufficientFee
			}
		}
	}
	m.removeConflicts(inputs)

	if m.txs.Len() >= maxSize {
		_, cheap, _ := m.txs.Peek()
		if op.GasFeeCap.Cmp(&cheap.GasPrice) <= 0 {
			return errInsufficientFee
		}
		m.removeConflicts(cheap.Inputs)
	}

	m.utxos.Put(op.ID, inputs)
	m.txs.Push(op.ID, &Transaction{
		ID:       op.ID,
		Tx:       tx,
		Inputs:   inputs,
		GasPrice: op.GasFeeCap,
		Op:       op,
	})
	return nil
}
