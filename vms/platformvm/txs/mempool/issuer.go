// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ txs.Visitor = &mempoolIssuer{}

	errCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	errCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")
)

type mempoolIssuer struct {
	m  *mempool
	tx *txs.Tx
}

func (i *mempoolIssuer) AdvanceTimeTx(tx *txs.AdvanceTimeTx) error {
	return errCantIssueAdvanceTimeTx
}

func (i *mempoolIssuer) RewardValidatorTx(tx *txs.RewardValidatorTx) error {
	return errCantIssueRewardValidatorTx
}

func (i *mempoolIssuer) AddValidatorTx(*txs.AddValidatorTx) error {
	i.m.addProposalTx(i.tx)
	return nil
}

func (i *mempoolIssuer) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	i.m.addProposalTx(i.tx)
	return nil
}

func (i *mempoolIssuer) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	i.m.addProposalTx(i.tx)
	return nil
}

func (i *mempoolIssuer) CreateChainTx(tx *txs.CreateChainTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *mempoolIssuer) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *mempoolIssuer) ImportTx(tx *txs.ImportTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}

func (i *mempoolIssuer) ExportTx(tx *txs.ExportTx) error {
	i.m.addDecisionTx(i.tx)
	return nil
}
