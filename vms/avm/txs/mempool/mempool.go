// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/avm/txs"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var _ Mempool = (*mempool)(nil)

// Mempool contains transactions that have not yet been put into a block.
type Mempool interface {
	txmempool.Mempool[*txs.Tx]

	// RequestBuildBlock notifies the consensus engine that a block should be
	// built if there is at least one transaction in the mempool.
	RequestBuildBlock()
}

type mempool struct {
	txmempool.Mempool[*txs.Tx]

	notify func()
}

func New(
	namespace string,
	registerer prometheus.Registerer,
	notify func(),
) (Mempool, error) {
	metrics, err := txmempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	pool := txmempool.New[*txs.Tx](
		metrics,
	)
	return &mempool{
		Mempool: pool,
		notify:  notify,
	}, nil
}

func (m *mempool) RequestBuildBlock() {
	if m.Len() == 0 {
		return
	}

	m.notify()
}
