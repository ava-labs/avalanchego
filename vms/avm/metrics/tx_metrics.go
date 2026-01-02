// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

const txLabel = "tx"

var (
	_ txs.Visitor = (*txMetrics)(nil)

	txLabels = []string{txLabel}
)

type txMetrics struct {
	numTxs *prometheus.CounterVec
}

func newTxMetrics(registerer prometheus.Registerer) (*txMetrics, error) {
	m := &txMetrics{
		numTxs: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "txs_accepted",
				Help: "number of transactions accepted",
			},
			txLabels,
		),
	}
	return m, registerer.Register(m.numTxs)
}

func (m *txMetrics) BaseTx(*txs.BaseTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "base",
	}).Inc()
	return nil
}

func (m *txMetrics) CreateAssetTx(*txs.CreateAssetTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "create_asset",
	}).Inc()
	return nil
}

func (m *txMetrics) OperationTx(*txs.OperationTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "operation",
	}).Inc()
	return nil
}

func (m *txMetrics) ImportTx(*txs.ImportTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "import",
	}).Inc()
	return nil
}

func (m *txMetrics) ExportTx(*txs.ExportTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "export",
	}).Inc()
	return nil
}
