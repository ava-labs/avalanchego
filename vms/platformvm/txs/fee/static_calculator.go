// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
)

var (
	_ backend = (*staticCalculator)(nil)

	errComplexityNotPriced = errors.New("complexity not priced")
)

func NewStaticCalculator(
	config StaticConfig,
	upgradeTimes upgrade.Config,
	chainTime time.Time,
) *Calculator {
	return &Calculator{
		b: &staticCalculator{
			upgrades:  upgradeTimes,
			staticCfg: config,
			time:      chainTime,
		},
	}
}

type staticCalculator struct {
	// inputs
	staticCfg StaticConfig
	upgrades  upgrade.Config
	time      time.Time

	// outputs of visitor execution
	fee uint64
}

func (c *staticCalculator) AddValidatorTx(*txs.AddValidatorTx) error {
	c.fee = c.staticCfg.AddPrimaryNetworkValidatorFee
	return nil
}

func (c *staticCalculator) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	c.fee = c.staticCfg.AddSubnetValidatorFee
	return nil
}

func (c *staticCalculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	c.fee = c.staticCfg.AddPrimaryNetworkDelegatorFee
	return nil
}

func (c *staticCalculator) CreateChainTx(*txs.CreateChainTx) error {
	if c.upgrades.IsApricotPhase3Activated(c.time) {
		c.fee = c.staticCfg.CreateBlockchainTxFee
	} else {
		c.fee = c.staticCfg.CreateAssetTxFee
	}
	return nil
}

func (c *staticCalculator) CreateSubnetTx(*txs.CreateSubnetTx) error {
	if c.upgrades.IsApricotPhase3Activated(c.time) {
		c.fee = c.staticCfg.CreateSubnetTxFee
	} else {
		c.fee = c.staticCfg.CreateAssetTxFee
	}
	return nil
}

func (c *staticCalculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	c.fee = 0 // no fees
	return nil
}

func (c *staticCalculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	c.fee = 0 // no fees
	return nil
}

func (c *staticCalculator) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *staticCalculator) TransformSubnetTx(*txs.TransformSubnetTx) error {
	c.fee = c.staticCfg.TransformSubnetTxFee
	return nil
}

func (c *staticCalculator) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *staticCalculator) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if tx.Subnet != constants.PrimaryNetworkID {
		c.fee = c.staticCfg.AddSubnetValidatorFee
	} else {
		c.fee = c.staticCfg.AddPrimaryNetworkValidatorFee
	}
	return nil
}

func (c *staticCalculator) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if tx.Subnet != constants.PrimaryNetworkID {
		c.fee = c.staticCfg.AddSubnetDelegatorFee
	} else {
		c.fee = c.staticCfg.AddPrimaryNetworkDelegatorFee
	}
	return nil
}

func (c *staticCalculator) BaseTx(*txs.BaseTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *staticCalculator) ImportTx(*txs.ImportTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *staticCalculator) ExportTx(*txs.ExportTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *staticCalculator) getFee() uint64 {
	return c.fee
}

func (c *staticCalculator) resetFee(newFee uint64) {
	c.fee = newFee
}

func (c *staticCalculator) computeFee(tx txs.UnsignedTx, _ []verify.Verifiable) (uint64, error) {
	c.fee = 0 // zero fee among different ComputeFee invocations (unlike gas which gets cumulated)
	err := tx.Visit(c)
	return c.fee, err
}

func (*staticCalculator) addFeesFor(fee.Dimensions) (uint64, error) {
	return 0, errComplexityNotPriced
}

func (*staticCalculator) removeFeesFor(fee.Dimensions) (uint64, error) {
	return 0, errComplexityNotPriced
}

func (*staticCalculator) getGasPrice() fee.GasPrice { return 0 }

func (*staticCalculator) getBlockGas() fee.Gas { return 0 }

func (*staticCalculator) getGasCap() fee.Gas { return 0 }

func (*staticCalculator) setCredentials([]verify.Verifiable) {}

func (*staticCalculator) isEActive() bool { return false }