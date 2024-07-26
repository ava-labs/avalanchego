// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
)

var (
	_ Calculator  = (*staticCalculator)(nil)
	_ txs.Visitor = (*staticVisitor)(nil)
)

func NewStaticCalculator(
	config StaticConfig,
	upgradeTimes upgrade.Config,
	chainTime time.Time,
) Calculator {
	return &staticCalculator{
		upgrades: upgradeTimes,
		config:   config,
		time:     chainTime,
	}
}

type staticCalculator struct {
	config   StaticConfig
	upgrades upgrade.Config
	time     time.Time
}

func (c *staticCalculator) CalculateFee(tx txs.UnsignedTx) (uint64, error) {
	v := staticVisitor{
		config:   c.config,
		upgrades: c.upgrades,
		time:     c.time,
	}
	err := tx.Visit(&v)
	return v.fee, err
}

type staticVisitor struct {
	// inputs
	config   StaticConfig
	upgrades upgrade.Config
	time     time.Time

	// outputs
	fee uint64
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
	if c.upgrades.IsApricotPhase3Activated(c.time) {
		c.fee = c.config.CreateBlockchainTxFee
	} else {
		c.fee = c.config.CreateAssetTxFee
	}
	return nil
}

func (c *staticVisitor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	if c.upgrades.IsApricotPhase3Activated(c.time) {
		c.fee = c.config.CreateSubnetTxFee
	} else {
		c.fee = c.config.CreateAssetTxFee
	}
	return nil
}

func (c *staticVisitor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	c.fee = 0 // no fees
	return nil
}

func (c *staticVisitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	c.fee = 0 // no fees
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
