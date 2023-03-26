// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

var _ txs.Visitor = (*txMetrics)(nil)

type txMetrics struct {
	numBaseTxs,
	numCreateAssetTxs,
	numOperationTxs,
	numImportTxs,
	numExportTxs prometheus.Counter
}

func newTxMetrics(
	namespace string,
	registerer prometheus.Registerer,
) (*txMetrics, error) {
	errs := wrappers.Errs{}
	m := &txMetrics{
		numBaseTxs:        newTxMetric(namespace, "base", registerer, &errs),
		numCreateAssetTxs: newTxMetric(namespace, "create_asset", registerer, &errs),
		numOperationTxs:   newTxMetric(namespace, "operation", registerer, &errs),
		numImportTxs:      newTxMetric(namespace, "import", registerer, &errs),
		numExportTxs:      newTxMetric(namespace, "export", registerer, &errs),
	}
	return m, errs.Err
}

func newTxMetric(
	namespace string,
	txName string,
	registerer prometheus.Registerer,
	errs *wrappers.Errs,
) prometheus.Counter {
	txMetric := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_txs_accepted", txName),
		Help:      fmt.Sprintf("Number of %s transactions accepted", txName),
	})
	errs.Add(registerer.Register(txMetric))
	return txMetric
}

func (m *txMetrics) BaseTx(*txs.BaseTx) error {
	m.numBaseTxs.Inc()
	return nil
}

func (m *txMetrics) CreateAssetTx(*txs.CreateAssetTx) error {
	m.numCreateAssetTxs.Inc()
	return nil
}

func (m *txMetrics) OperationTx(*txs.OperationTx) error {
	m.numOperationTxs.Inc()
	return nil
}

func (m *txMetrics) ImportTx(*txs.ImportTx) error {
	m.numImportTxs.Inc()
	return nil
}

func (m *txMetrics) ExportTx(*txs.ExportTx) error {
	m.numExportTxs.Inc()
	return nil
}
