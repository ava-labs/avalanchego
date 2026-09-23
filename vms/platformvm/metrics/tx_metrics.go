// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

const txLabel = "tx"

var (
	_ platform.TxVisitor = (*txMetrics)(nil)

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

func (m *txMetrics) AddValidatorTx(*platform.AddValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) AddSubnetValidatorTx(*platform.AddSubnetValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_subnet_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) AddDelegatorTx(*platform.AddDelegatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_delegator",
	}).Inc()
	return nil
}

func (m *txMetrics) CreateChainTx(*platform.CreateChainTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "create_chain",
	}).Inc()
	return nil
}

func (m *txMetrics) CreateSubnetTx(*platform.CreateSubnetTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "create_subnet",
	}).Inc()
	return nil
}

func (m *txMetrics) ImportTx(*platform.ImportTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "import",
	}).Inc()
	return nil
}

func (m *txMetrics) ExportTx(*platform.ExportTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "export",
	}).Inc()
	return nil
}

func (m *txMetrics) AdvanceTimeTx(*platform.AdvanceTimeTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "advance_time",
	}).Inc()
	return nil
}

func (m *txMetrics) RewardValidatorTx(*platform.RewardValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "reward_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) RemoveSubnetValidatorTx(*platform.RemoveSubnetValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "remove_subnet_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) TransformSubnetTx(*platform.TransformSubnetTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "transform_subnet",
	}).Inc()
	return nil
}

func (m *txMetrics) AddPermissionlessValidatorTx(*platform.AddPermissionlessValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_permissionless_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) AddPermissionlessDelegatorTx(*platform.AddPermissionlessDelegatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_permissionless_delegator",
	}).Inc()
	return nil
}

func (m *txMetrics) TransferSubnetOwnershipTx(*platform.TransferSubnetOwnershipTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "transfer_subnet_ownership",
	}).Inc()
	return nil
}

func (m *txMetrics) BaseTx(*platform.BaseTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "base",
	}).Inc()
	return nil
}

func (m *txMetrics) ConvertSubnetToL1Tx(*platform.ConvertSubnetToL1Tx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "convert_subnet_to_l1",
	}).Inc()
	return nil
}

func (m *txMetrics) RegisterL1ValidatorTx(*platform.RegisterL1ValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "register_l1_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) SetL1ValidatorWeightTx(*platform.SetL1ValidatorWeightTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "set_l1_validator_weight",
	}).Inc()
	return nil
}

func (m *txMetrics) IncreaseL1ValidatorBalanceTx(*platform.IncreaseL1ValidatorBalanceTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "increase_l1_validator_balance",
	}).Inc()
	return nil
}

func (m *txMetrics) DisableL1ValidatorTx(*platform.DisableL1ValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "disable_l1_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) AddAutoRenewedValidatorTx(*platform.AddAutoRenewedValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_auto_renewed_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) SetAutoRenewedValidatorConfigTx(*platform.SetAutoRenewedValidatorConfigTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "set_auto_renewed_validator_config",
	}).Inc()
	return nil
}

func (m *txMetrics) RewardAutoRenewedValidatorTx(*platform.RewardAutoRenewedValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "reward_auto_renewed_validator",
	}).Inc()
	return nil
}
