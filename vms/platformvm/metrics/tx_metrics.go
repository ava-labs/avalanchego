// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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

func (m *txMetrics) AddValidatorTx(*txs.AddValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_subnet_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) AddDelegatorTx(*txs.AddDelegatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_delegator",
	}).Inc()
	return nil
}

func (m *txMetrics) CreateChainTx(*txs.CreateChainTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "create_chain",
	}).Inc()
	return nil
}

func (m *txMetrics) CreateSubnetTx(*txs.CreateSubnetTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "create_subnet",
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

func (m *txMetrics) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "advance_time",
	}).Inc()
	return nil
}

func (m *txMetrics) RewardValidatorTx(*txs.RewardValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "reward_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "remove_subnet_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) TransformSubnetTx(*txs.TransformSubnetTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "transform_subnet",
	}).Inc()
	return nil
}

func (m *txMetrics) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_permissionless_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "add_permissionless_delegator",
	}).Inc()
	return nil
}

func (m *txMetrics) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "transfer_subnet_ownership",
	}).Inc()
	return nil
}

func (m *txMetrics) BaseTx(*txs.BaseTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "base",
	}).Inc()
	return nil
}

func (m *txMetrics) ConvertSubnetToL1Tx(*txs.ConvertSubnetToL1Tx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "convert_subnet_to_l1",
	}).Inc()
	return nil
}

func (m *txMetrics) RegisterL1ValidatorTx(*txs.RegisterL1ValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "register_l1_validator",
	}).Inc()
	return nil
}

func (m *txMetrics) SetL1ValidatorWeightTx(*txs.SetL1ValidatorWeightTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "set_l1_validator_weight",
	}).Inc()
	return nil
}

func (m *txMetrics) IncreaseL1ValidatorBalanceTx(*txs.IncreaseL1ValidatorBalanceTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "increase_l1_validator_balance",
	}).Inc()
	return nil
}

func (m *txMetrics) DisableL1ValidatorTx(*txs.DisableL1ValidatorTx) error {
	m.numTxs.With(prometheus.Labels{
		txLabel: "disable_l1_validator",
	}).Inc()
	return nil
}
