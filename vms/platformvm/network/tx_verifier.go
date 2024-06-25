// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ TxVerifier = (*LockedTxVerifier)(nil)

type TxVerifier interface {
	// VerifyTx verifies that the transaction should be issued into the mempool.
	VerifyTx(ctx context.Context, tx *txs.Tx) error
}

type LockedTxVerifier struct {
	lock       sync.Locker
	txVerifier TxVerifier
}

func (l *LockedTxVerifier) VerifyTx(ctx context.Context, tx *txs.Tx) error {
	l.lock.Lock()
	defer l.lock.Unlock()

	return l.txVerifier.VerifyTx(ctx, tx)
}

func NewLockedTxVerifier(lock sync.Locker, txVerifier TxVerifier) *LockedTxVerifier {
	return &LockedTxVerifier{
		lock:       lock,
		txVerifier: txVerifier,
	}
}
