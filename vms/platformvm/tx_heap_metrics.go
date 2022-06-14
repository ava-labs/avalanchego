// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ TxHeap = &txHeapWithMetrics{}

type txHeapWithMetrics struct {
	TxHeap

	numTxs prometheus.Gauge
}

func NewTxHeapWithMetrics(
	txHeap TxHeap,
	namespace string,
	registerer prometheus.Registerer,
) (TxHeap, error) {
	h := &txHeapWithMetrics{
		TxHeap: txHeap,
		numTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "count",
			Help:      "Number of transactions in the heap",
		}),
	}
	return h, registerer.Register(h.numTxs)
}

func (h *txHeapWithMetrics) Add(tx *txs.Tx) {
	h.TxHeap.Add(tx)
	h.numTxs.Set(float64(h.TxHeap.Len()))
}

func (h *txHeapWithMetrics) Remove(txID ids.ID) *txs.Tx {
	tx := h.TxHeap.Remove(txID)
	h.numTxs.Set(float64(h.TxHeap.Len()))
	return tx
}

func (h *txHeapWithMetrics) RemoveTop() *txs.Tx {
	tx := h.TxHeap.RemoveTop()
	h.numTxs.Set(float64(h.TxHeap.Len()))
	return tx
}
