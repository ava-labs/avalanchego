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
	numClaimTxs,
	numRegisterNodeTxs,
	numRewardsImportTxs,
	numBaseTxs,
	numMultisigAliasTxs,
	numAddDepositOfferTxs prometheus.Counter
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
		numAddressStateTxs:    newTxMetric(namespace, "add_address_state", registerer, &errs),
		numDepositTxs:         newTxMetric(namespace, "deposit", registerer, &errs),
		numUnlockDepositTxs:   newTxMetric(namespace, "unlock_deposit", registerer, &errs),
		numClaimTxs:           newTxMetric(namespace, "claim", registerer, &errs),
		numRegisterNodeTxs:    newTxMetric(namespace, "register_node", registerer, &errs),
		numRewardsImportTxs:   newTxMetric(namespace, "rewards_import", registerer, &errs),
		numBaseTxs:            newTxMetric(namespace, "base", registerer, &errs),
		numMultisigAliasTxs:   newTxMetric(namespace, "multisig_alias", registerer, &errs),
		numAddDepositOfferTxs: newTxMetric(namespace, "add_deposit_offer", registerer, &errs),
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

func (*txMetrics) ClaimTx(*txs.ClaimTx) error {
	return nil
}

func (*txMetrics) RegisterNodeTx(*txs.RegisterNodeTx) error {
	return nil
}

func (*txMetrics) RewardsImportTx(*txs.RewardsImportTx) error {
	return nil
}

func (*txMetrics) BaseTx(*txs.BaseTx) error {
	return nil
}

func (*txMetrics) MultisigAliasTx(*txs.MultisigAliasTx) error {
	return nil
}

func (*txMetrics) AddDepositOfferTx(*txs.AddDepositOfferTx) error {
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

func (m *caminoTxMetrics) ClaimTx(*txs.ClaimTx) error {
	m.numClaimTxs.Inc()
	return nil
}

func (m *caminoTxMetrics) RegisterNodeTx(*txs.RegisterNodeTx) error {
	m.numRegisterNodeTxs.Inc()
	return nil
}

func (m *caminoTxMetrics) RewardsImportTx(*txs.RewardsImportTx) error {
	m.numRewardsImportTxs.Inc()
	return nil
}

func (m *caminoTxMetrics) BaseTx(*txs.BaseTx) error {
	m.numBaseTxs.Inc()
	return nil
}

func (m *caminoTxMetrics) MultisigAliasTx(*txs.MultisigAliasTx) error {
	m.numMultisigAliasTxs.Inc()
	return nil
}

func (m *caminoTxMetrics) AddDepositOfferTx(*txs.AddDepositOfferTx) error {
	m.numAddDepositOfferTxs.Inc()
	return nil
}
