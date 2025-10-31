// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	metricspkg "github.com/ava-labs/libevm/metrics"
)

type metrics struct {
	pendingTxs metricspkg.Gauge // Gauge of currently pending transactions in the txHeap
	currentTxs metricspkg.Gauge // Gauge of current transactions to be issued into a block
	issuedTxs  metricspkg.Gauge // Gauge of transactions that have been issued into a block

	addedTxs     metricspkg.Counter // Count of all transactions added to the mempool
	discardedTxs metricspkg.Counter // Count of all discarded transactions
}

func newMetrics() *metrics {
	return &metrics{
		pendingTxs:   metricspkg.GetOrRegisterGauge("atomic_mempool_pending_txs", nil),
		currentTxs:   metricspkg.GetOrRegisterGauge("atomic_mempool_current_txs", nil),
		issuedTxs:    metricspkg.GetOrRegisterGauge("atomic_mempool_issued_txs", nil),
		addedTxs:     metricspkg.GetOrRegisterCounter("atomic_mempool_added_txs", nil),
		discardedTxs: metricspkg.GetOrRegisterCounter("atomic_mempool_discarded_txs", nil),
	}
}
