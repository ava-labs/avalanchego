// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
)

var _ txs.Visitor = (*Calculator)(nil)

type Calculator struct {
	// Pre E-fork inputs
	upgrades  upgrade.Times
	staticCfg StaticConfig
	chainTime time.Time

	// outputs of visitor execution
	Fee uint64
}

func NewStaticCalculator(cfg StaticConfig, ut upgrade.Times, chainTime time.Time) *Calculator {
	return &Calculator{
		upgrades:  ut,
		staticCfg: cfg,
		chainTime: chainTime,
	}
}

func (fc *Calculator) AddValidatorTx(*txs.AddValidatorTx) error {
	fc.Fee = fc.staticCfg.AddPrimaryNetworkValidatorFee
	return nil
}

func (fc *Calculator) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	fc.Fee = fc.staticCfg.AddSubnetValidatorFee
	return nil
}

func (fc *Calculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	fc.Fee = fc.staticCfg.AddPrimaryNetworkDelegatorFee
	return nil
}

func (fc *Calculator) CreateChainTx(*txs.CreateChainTx) error {
	fc.Fee = fc.staticCfg.GetCreateBlockchainTxFee(fc.upgrades, fc.chainTime)
	return nil
}

func (fc *Calculator) CreateSubnetTx(*txs.CreateSubnetTx) error {
	fc.Fee = fc.staticCfg.GetCreateSubnetTxFee(fc.upgrades, fc.chainTime)
	return nil
}

func (*Calculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil // no fees
}

func (*Calculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil // no fees
}

func (fc *Calculator) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	fc.Fee = fc.staticCfg.TxFee
	return nil
}

func (fc *Calculator) TransformSubnetTx(*txs.TransformSubnetTx) error {
	fc.Fee = fc.staticCfg.TransformSubnetTxFee
	return nil
}

func (fc *Calculator) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	fc.Fee = fc.staticCfg.TxFee
	return nil
}

func (fc *Calculator) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if tx.Subnet != constants.PrimaryNetworkID {
		fc.Fee = fc.staticCfg.AddSubnetValidatorFee
	} else {
		fc.Fee = fc.staticCfg.AddPrimaryNetworkValidatorFee
	}
	return nil
}

func (fc *Calculator) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if tx.Subnet != constants.PrimaryNetworkID {
		fc.Fee = fc.staticCfg.AddSubnetDelegatorFee
	} else {
		fc.Fee = fc.staticCfg.AddPrimaryNetworkDelegatorFee
	}
	return nil
}

func (fc *Calculator) BaseTx(*txs.BaseTx) error {
	fc.Fee = fc.staticCfg.TxFee
	return nil
}

func (fc *Calculator) ImportTx(*txs.ImportTx) error {
	fc.Fee = fc.staticCfg.TxFee
	return nil
}

func (fc *Calculator) ExportTx(*txs.ExportTx) error {
	fc.Fee = fc.staticCfg.TxFee
	return nil
}
