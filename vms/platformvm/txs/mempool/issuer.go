// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ txs.Visitor = (*issuer)(nil)

	errCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	errCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")
)

type issuer struct {
	m  *mempool
	tx *txs.Tx
}

func (i *issuer) AdvanceTimeTx(tx *txs.AdvanceTimeTx) error {
	return errCantIssueAdvanceTimeTx
}

func (i *issuer) RewardValidatorTx(tx *txs.RewardValidatorTx) error {
	return errCantIssueRewardValidatorTx
}

func (i *issuer) AddValidatorTx(*txs.AddValidatorTx) error {
	i.m.addStakerTx(i.tx)
	return nil
}

func (i *issuer) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	i.m.addStakerTx(i.tx)
	return nil
}

func (i *issuer) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	i.m.addStakerTx(i.tx)
	return nil
}

func (i *issuer) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) CreateChainTx(tx *txs.CreateChainTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) ImportTx(tx *txs.ImportTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) ExportTx(tx *txs.ExportTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *issuer) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	i.m.addStakerTx(i.tx)
	return nil
}

func (i *issuer) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	i.m.addStakerTx(i.tx)
	return nil
}
