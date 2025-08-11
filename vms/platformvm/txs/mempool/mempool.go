// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
}

func New(namespace string, registerer prometheus.Registerer) (*Mempool, error) {
	metrics, err := txmempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	pool := txmempool.New[*txs.Tx](
		metrics,
	)
	return &Mempool{Mempool: pool}, nil
}

func (m *Mempool) Add(tx *txs.Tx) error {
	switch tx.Unsigned.(type) {
	case *txs.AdvanceTimeTx:
		return ErrCantIssueAdvanceTimeTx
	case *txs.RewardValidatorTx:
		return ErrCantIssueRewardValidatorTx
	default:
		return m.Mempool.Add(tx)
	}
}
