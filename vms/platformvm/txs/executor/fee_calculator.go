// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fees"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ txs.Visitor = (*FeeCalculator)(nil)

	errFailedFeeCalculation          = errors.New("failed fee calculation")
	errFailedConsumedUnitsCumulation = errors.New("failed cumulating consumed units")
)

type FeeCalculator struct {
	// inputs, to be filled before visitor methods are called
	Config     *config.Config
	ChainTime  time.Time
	Tx         *txs.Tx
	feeManager *fees.Manager

	// outputs of visitor execution
	Fee uint64
}

func (fc *FeeCalculator) AddValidatorTx(*txs.AddValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddPrimaryNetworkValidatorFee
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddSubnetValidatorFee
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddPrimaryNetworkDelegatorFee
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) CreateChainTx(*txs.CreateChainTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.GetCreateBlockchainTxFee(fc.ChainTime)
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) CreateSubnetTx(*txs.CreateSubnetTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.GetCreateSubnetTxFee(fc.ChainTime)
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (*FeeCalculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil // no fees
}

func (*FeeCalculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil // no fees
}

func (fc *FeeCalculator) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) TransformSubnetTx(*txs.TransformSubnetTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TransformSubnetTxFee
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		if tx.Subnet != constants.PrimaryNetworkID {
			fc.Fee = fc.Config.AddSubnetValidatorFee
		} else {
			fc.Fee = fc.Config.AddPrimaryNetworkValidatorFee
		}
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		if tx.Subnet != constants.PrimaryNetworkID {
			fc.Fee = fc.Config.AddSubnetDelegatorFee
		} else {
			fc.Fee = fc.Config.AddPrimaryNetworkDelegatorFee
		}
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) BaseTx(*txs.BaseTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) ImportTx(*txs.ImportTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) ExportTx(*txs.ExportTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits := commonConsumedUnits(fc.Tx)

	var err error
	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func commonConsumedUnits(tx *txs.Tx) fees.Dimensions {
	var consumedUnits fees.Dimensions
	consumedUnits[fees.Bandwidth] = uint64(len(tx.Bytes()))

	// TODO ABENEGIA: consider accounting for input complexity
	// TODO ABENEGIA: consider handling imports/exports differently
	insCount := tx.Unsigned.InputIDs().Len()
	outsCount := len(tx.Unsigned.Outputs())
	consumedUnits[fees.UTXORead] = uint64(insCount)              // inputs are read
	consumedUnits[fees.UTXOWrite] = uint64(insCount + outsCount) // inputs are deleted, outputs are created
	return consumedUnits
}

func processFees(cfg *config.Config, chainTime time.Time, fc *fees.Manager, consumedUnits fees.Dimensions) (uint64, error) {
	boundBreached, dimension := fc.CumulateUnits(consumedUnits, cfg.BlockMaxConsumedUnits(chainTime))
	if boundBreached {
		return 0, fmt.Errorf("%w: breached dimension %d", errFailedConsumedUnitsCumulation, dimension)
	}

	fee, err := fc.CalculateFee(consumedUnits)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}

	return fee, nil
}
