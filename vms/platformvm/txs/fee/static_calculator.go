// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Calculator  = (*staticCalculator)(nil)
	_ txs.Visitor = (*staticVisitor)(nil)
)

func NewStaticCalculator(config StaticConfig) Calculator {
	return &staticCalculator{
		config: config,
	}
}

type staticCalculator struct {
	config StaticConfig
}

func (c *staticCalculator) CalculateFee(tx txs.UnsignedTx) (uint64, error) {
	v := staticVisitor{
		config: c.config,
	}
	err := tx.Visit(&v)
	return v.fee, err
}

type staticVisitor struct {
	// inputs
	config StaticConfig

	// outputs
	fee uint64
}

func (*staticVisitor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrUnsupportedTx
}

func (*staticVisitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrUnsupportedTx
}

func (*staticVisitor) ConvertSubnetTx(*txs.ConvertSubnetTx) error {
	return ErrUnsupportedTx
}

func (c *staticVisitor) AddValidatorTx(*txs.AddValidatorTx) error {
	c.fee = c.config.AddPrimaryNetworkValidatorFee
	return nil
}

func (c *staticVisitor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	c.fee = c.config.AddSubnetValidatorFee
	return nil
}

func (c *staticVisitor) AddDelegatorTx(*txs.AddDelegatorTx) error {
	c.fee = c.config.AddPrimaryNetworkDelegatorFee
	return nil
}

func (c *staticVisitor) CreateChainTx(*txs.CreateChainTx) error {
	c.fee = c.config.CreateBlockchainTxFee
	return nil
}

func (c *staticVisitor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	c.fee = c.config.CreateSubnetTxFee
	return nil
}

func (c *staticVisitor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (c *staticVisitor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	c.fee = c.config.TransformSubnetTxFee
	return nil
}

func (c *staticVisitor) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (c *staticVisitor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if tx.Subnet != constants.PrimaryNetworkID {
		c.fee = c.config.AddSubnetValidatorFee
	} else {
		c.fee = c.config.AddPrimaryNetworkValidatorFee
	}
	return nil
}

func (c *staticVisitor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if tx.Subnet != constants.PrimaryNetworkID {
		c.fee = c.config.AddSubnetDelegatorFee
	} else {
		c.fee = c.config.AddPrimaryNetworkDelegatorFee
	}
	return nil
}

func (c *staticVisitor) BaseTx(*txs.BaseTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (c *staticVisitor) ImportTx(*txs.ImportTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (c *staticVisitor) ExportTx(*txs.ExportTx) error {
	c.fee = c.config.TxFee
	return nil
}
