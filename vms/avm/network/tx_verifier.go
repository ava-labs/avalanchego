// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"sync"

	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

var _ TxVerifier = (*lockedTxVerifier)(nil)

type TxVerifier interface {
	// VerifyTx verifies that the transaction should be issued into the mempool.
	VerifyTx(tx *txs.Tx) error
}

type lockedTxVerifier struct {
	lock       sync.Locker
	txVerifier TxVerifier
}

func (l *lockedTxVerifier) VerifyTx(tx *txs.Tx) error {
	l.lock.Lock()
	defer l.lock.Unlock()

	return l.txVerifier.VerifyTx(tx)
}

func NewLockedTxVerifier(lock sync.Locker, txVerifier TxVerifier) TxVerifier {
	return &lockedTxVerifier{
		lock:       lock,
		txVerifier: txVerifier,
	}
}
