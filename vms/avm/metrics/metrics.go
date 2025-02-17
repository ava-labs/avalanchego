// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

var _ Metrics = (*metrics)(nil)

type Metrics interface {
	metric.APIInterceptor

	IncTxRefreshes()
	IncTxRefreshHits()
	IncTxRefreshMisses()

	// MarkBlockAccepted updates all metrics relating to the acceptance of a
	// block, including the underlying acceptance of the contained transactions.
	MarkBlockAccepted(b block.Block) error
	// MarkTxAccepted updates all metrics relating to the acceptance of a
	// transaction.
	//
	// Note: This is not intended to be called during the acceptance of a block,
	// as MarkBlockAccepted already handles updating transaction related
	// metrics.
	MarkTxAccepted(tx *txs.Tx) error
}

type metrics struct {
	txMetrics *txMetrics

	numTxRefreshes, numTxRefreshHits, numTxRefreshMisses prometheus.Counter

	metric.APIInterceptor
}

func (m *metrics) IncTxRefreshes() {
	m.numTxRefreshes.Inc()
}

func (m *metrics) IncTxRefreshHits() {
	m.numTxRefreshHits.Inc()
}

func (m *metrics) IncTxRefreshMisses() {
	m.numTxRefreshMisses.Inc()
}

func (m *metrics) MarkBlockAccepted(b block.Block) error {
	for _, tx := range b.Txs() {
		if err := tx.Unsigned.Visit(m.txMetrics); err != nil {
			return err
		}
	}
	return nil
}

func (m *metrics) MarkTxAccepted(tx *txs.Tx) error {
	return tx.Unsigned.Visit(m.txMetrics)
}

func New(registerer prometheus.Registerer) (Metrics, error) {
	txMetrics, err := newTxMetrics(registerer)
	errs := wrappers.Errs{Err: err}

	m := &metrics{txMetrics: txMetrics}

	m.numTxRefreshes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_refreshes",
		Help: "Number of times unique txs have been refreshed",
	})
	m.numTxRefreshHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_refresh_hits",
		Help: "Number of times unique txs have not been unique, but were cached",
	})
	m.numTxRefreshMisses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_refresh_misses",
		Help: "Number of times unique txs have not been unique and weren't cached",
	})

	apiRequestMetric, err := metric.NewAPIInterceptor(registerer)
	m.APIInterceptor = apiRequestMetric
	errs.Add(
		err,
		registerer.Register(m.numTxRefreshes),
		registerer.Register(m.numTxRefreshHits),
		registerer.Register(m.numTxRefreshMisses),
	)
	return m, errs.Err
}
