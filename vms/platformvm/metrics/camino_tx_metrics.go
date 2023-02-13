// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
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
	numAddressStateTxs,
	numDepositTxs,
	numUnlockDepositTxs,
	numClaimRewardTxs,
	numRegisterNodeTxs,
	numRewardsImportTxs prometheus.Counter
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
		numAddressStateTxs:  newTxMetric(namespace, "add_address_state", registerer, &errs),
		numDepositTxs:       newTxMetric(namespace, "deposit", registerer, &errs),
		numUnlockDepositTxs: newTxMetric(namespace, "unlock_deposit", registerer, &errs),
		numClaimRewardTxs:   newTxMetric(namespace, "claim_reward", registerer, &errs),
		numRegisterNodeTxs:  newTxMetric(namespace, "register_node", registerer, &errs),
		numRewardsImportTxs: newTxMetric(namespace, "rewards_import", registerer, &errs),
	}
	return m, errs.Err
}

// avax metrics

func (*txMetrics) AddressStateTx(*txs.AddressStateTx) error {
	return nil
}

func (*txMetrics) DepositTx(*txs.DepositTx) error {
	return nil
}

func (*txMetrics) UnlockDepositTx(*txs.UnlockDepositTx) error {
	return nil
}

func (*txMetrics) ClaimRewardTx(*txs.ClaimRewardTx) error {
	return nil
}

func (*txMetrics) RegisterNodeTx(*txs.RegisterNodeTx) error {
	return nil
}

func (*txMetrics) RewardsImportTx(*txs.RewardsImportTx) error {
	return nil
}

// camino metrics

func (m *caminoTxMetrics) AddressStateTx(*txs.AddressStateTx) error {
	m.numAddressStateTxs.Inc()
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

func (m *caminoTxMetrics) ClaimRewardTx(*txs.ClaimRewardTx) error {
	m.numClaimRewardTxs.Inc()
	return nil
}

func (m *caminoTxMetrics) RegisterNodeTx(*txs.RegisterNodeTx) error {
	m.numRegisterNodeTxs.Inc()
	return nil
}

func (m *caminoTxMetrics) RewardsImportTx(*txs.RewardsImportTx) error {
	m.numRegisterNodeTxs.Inc()
	return nil
}
