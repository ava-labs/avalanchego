// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/prometheus/client_golang/prometheus"
)

var _ txs.Visitor = (*caminoTxMetrics)(nil)

type caminoTxMetrics struct {
	txMetrics
	numAddAddressStateTxs,
	numDepositTxs,
	numUnlockDepositTxs prometheus.Counter
}

func newCaminoTxMetrics(
	namespace string,
	registerer prometheus.Registerer,
) (*caminoTxMetrics, error) {
	txm, err := newTxMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}

	errs := wrappers.Errs{}
	m := &caminoTxMetrics{
		txMetrics: *txm,
		// Camino specific tx metrics
		numAddAddressStateTxs: newTxMetric(namespace, "add_address_state", registerer, &errs),
	}
	return m, errs.Err
}

func (m *txMetrics) AddAddressStateTx(*txs.AddAddressStateTx) error { return nil }
func (m *txMetrics) DepositTx(*txs.DepositTx) error                 { return nil }
func (m *txMetrics) UnlockDepositTx(*txs.UnlockDepositTx) error     { return nil }

func (m *caminoTxMetrics) AddAddressStateTx(*txs.AddAddressStateTx) error {
	m.numAddAddressStateTxs.Inc()
	return nil
}

func (m *caminoTxMetrics) DepositTx(*txs.DepositTx) error {
	m.numDepositTxs.Inc()
	return nil
}

func (m *caminoTxMetrics) UnlockDepositTx(*txs.UnlockDepositTx) error {
	m.numUnlockDepositTxs.Inc()
	return nil
}
