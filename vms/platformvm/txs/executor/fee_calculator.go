// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fees"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ txs.Visitor = (*FeeCalculator)(nil)

	errNotYetImplemented             = errors.New("not yet implemented")
	errFailedFeeCalculation          = errors.New("failed fee calculation")
	errFailedConsumedUnitsCumulation = errors.New("failed cumulating consumed units")
)

type FeeCalculator struct {
	// inputs, to be filled before visitor methods are called
	*Backend
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

	var (
		consumedUnits fees.Dimensions
		err           error
	)
	consumedUnits[fees.Bandwidth] = uint64(len(fc.Tx.Bytes()))

	fc.Fee, err = processFees(fc.Config, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddSubnetValidatorFee
		return nil
	}

	var (
		consumedUnits fees.Dimensions
		err           error
	)
	consumedUnits[fees.Bandwidth] = uint64(len(fc.Tx.Bytes()))

	fc.Fee, err = processFees(fc.Config, fc.feeManager, consumedUnits)
	return err
}

func (*FeeCalculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) CreateChainTx(*txs.CreateChainTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) BaseTx(*txs.BaseTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) ImportTx(*txs.ImportTx) error {
	return errNotYetImplemented
}

func (*FeeCalculator) ExportTx(*txs.ExportTx) error {
	return errNotYetImplemented
}

func processFees(cfg *config.Config, fc *fees.Manager, consumedUnits fees.Dimensions) (uint64, error) {
	boundBreached, dimension := fc.CumulateUnits(consumedUnits, cfg.BlockMaxConsumedUnits())
	if boundBreached {
		return 0, fmt.Errorf("%w: breached dimension %d", errFailedConsumedUnitsCumulation, dimension)
	}

	fee, err := fc.CalculateFee(consumedUnits)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}

	return fee, nil
}
