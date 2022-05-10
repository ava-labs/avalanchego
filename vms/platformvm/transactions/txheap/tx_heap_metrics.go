// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
)

var _ Heap = &txHeapWithMetrics{}

type txHeapWithMetrics struct {
	Heap

	numTxs prometheus.Gauge
}

func NewTxHeapWithMetrics(
	txHeap Heap,
	namespace string,
	registerer prometheus.Registerer,
) (Heap, error) {
	h := &txHeapWithMetrics{
		Heap: txHeap,
		numTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "count",
			Help:      "Number of transactions in the heap",
		}),
	}
	return h, registerer.Register(h.numTxs)
}

func (h *txHeapWithMetrics) Add(tx *signed.Tx) {
	h.Heap.Add(tx)
	h.numTxs.Set(float64(h.Heap.Len()))
}

func (h *txHeapWithMetrics) Remove(txID ids.ID) *signed.Tx {
	tx := h.Heap.Remove(txID)
	h.numTxs.Set(float64(h.Heap.Len()))
	return tx
}

func (h *txHeapWithMetrics) RemoveTop() *signed.Tx {
	tx := h.Heap.RemoveTop()
	h.numTxs.Set(float64(h.Heap.Len()))
	return tx
}
