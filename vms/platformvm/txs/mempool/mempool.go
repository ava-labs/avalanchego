// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	ErrCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	ErrCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")
)

type Mempool struct {
	txmempool.Mempool[*txs.Tx]
	notify func() // Notifies when a transaction is added to the mempool
}

func New(namespace string, registerer prometheus.Registerer, notify func()) (*Mempool, error) {
	metrics, err := txmempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	pool := txmempool.New[*txs.Tx](
		metrics,
	)
	return &Mempool{Mempool: pool, notify: notify}, nil
}

func (m *Mempool) Add(tx *txs.Tx) error {
	switch tx.Unsigned.(type) {
	case *txs.AdvanceTimeTx:
		return ErrCantIssueAdvanceTimeTx
	case *txs.RewardValidatorTx:
		return ErrCantIssueRewardValidatorTx
	default:
	}

	err := m.Mempool.Add(tx)
	if err == nil {
		m.notify()
	}
	return err
}
