// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ txs.Visitor = (*TxExpectsContext)(nil)

func ExpectsContext(blk block.Block) (bool, error) {
	t := TxExpectsContext{}
	for _, tx := range blk.Txs() {
		if err := tx.Unsigned.Visit(&t); err != nil {
			return false, err
		}
	}
	return t.Result, nil
}

type TxExpectsContext struct {
	Result bool
}

func (*TxExpectsContext) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return nil
}

func (*TxExpectsContext) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return nil
}

func (*TxExpectsContext) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return nil
}

func (*TxExpectsContext) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return nil
}

func (*TxExpectsContext) AddValidatorTx(*txs.AddValidatorTx) error {
	return nil
}

func (*TxExpectsContext) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil
}

func (*TxExpectsContext) BaseTx(*txs.BaseTx) error {
	return nil
}

func (*TxExpectsContext) CreateChainTx(*txs.CreateChainTx) error {
	return nil
}

func (*TxExpectsContext) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return nil
}

func (*TxExpectsContext) ExportTx(*txs.ExportTx) error {
	return nil
}

func (*TxExpectsContext) ImportTx(*txs.ImportTx) error {
	return nil
}

func (*TxExpectsContext) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return nil
}

func (*TxExpectsContext) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil
}

func (*TxExpectsContext) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return nil
}

func (*TxExpectsContext) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return nil
}
