// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
)

var _ txs.Visitor = (*calculator)(nil)

func NewStaticCalculator(config StaticConfig, upgradeTimes upgrade.Config) *Calculator {
	return &Calculator{
		config:       config,
		upgradeTimes: upgradeTimes,
	}
}

type Calculator struct {
	config       StaticConfig
	upgradeTimes upgrade.Config
}

// [CalculateFee] returns the minimal fee needed to accept [tx], at chain time [time]
func (c *Calculator) CalculateFee(tx txs.UnsignedTx, time time.Time) uint64 {
	tmp := &calculator{
		upgrades:  c.upgradeTimes,
		staticCfg: c.config,
		time:      time,
	}

	// this is guaranteed to never return an error
	_ = tx.Visit(tmp)
	return tmp.fee
}

// calculator is intentionally unexported and used through Calculator to provide
// a more convenient API
type calculator struct {
	// Pre E-fork inputs
	upgrades  upgrade.Config
	staticCfg StaticConfig
	time      time.Time

	// outputs of visitor execution
	fee uint64
}

func (c *calculator) AddValidatorTx(*txs.AddValidatorTx) error {
	c.fee = c.staticCfg.AddPrimaryNetworkValidatorFee
	return nil
}

func (c *calculator) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	c.fee = c.staticCfg.AddSubnetValidatorFee
	return nil
}

func (c *calculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	c.fee = c.staticCfg.AddPrimaryNetworkDelegatorFee
	return nil
}

func (c *calculator) CreateChainTx(*txs.CreateChainTx) error {
	if c.upgrades.IsApricotPhase3Activated(c.time) {
		c.fee = c.staticCfg.CreateBlockchainTxFee
	} else {
		c.fee = c.staticCfg.CreateAssetTxFee
	}
	return nil
}

func (c *calculator) CreateSubnetTx(*txs.CreateSubnetTx) error {
	if c.upgrades.IsApricotPhase3Activated(c.time) {
		c.fee = c.staticCfg.CreateSubnetTxFee
	} else {
		c.fee = c.staticCfg.CreateAssetTxFee
	}
	return nil
}

func (c *calculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	c.fee = 0 // no fees
	return nil
}

func (c *calculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	c.fee = 0 // no fees
	return nil
}

func (c *calculator) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *calculator) TransformSubnetTx(*txs.TransformSubnetTx) error {
	c.fee = c.staticCfg.TransformSubnetTxFee
	return nil
}

func (c *calculator) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *calculator) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if tx.Subnet != constants.PrimaryNetworkID {
		c.fee = c.staticCfg.AddSubnetValidatorFee
	} else {
		c.fee = c.staticCfg.AddPrimaryNetworkValidatorFee
	}
	return nil
}

func (c *calculator) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if tx.Subnet != constants.PrimaryNetworkID {
		c.fee = c.staticCfg.AddSubnetDelegatorFee
	} else {
		c.fee = c.staticCfg.AddPrimaryNetworkDelegatorFee
	}
	return nil
}

func (c *calculator) BaseTx(*txs.BaseTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *calculator) ImportTx(*txs.ImportTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *calculator) ExportTx(*txs.ExportTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}
