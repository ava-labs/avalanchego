// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
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
	// setup, to be filled before visitor methods are called
	feeManager *fees.Manager
	Config     *config.Config
	ChainTime  time.Time

	// inputs, to be filled before visitor methods are called
	Credentials []verify.Verifiable

	// outputs of visitor execution
	Fee uint64
}

func (fc *FeeCalculator) AddValidatorTx(tx *txs.AddValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddPrimaryNetworkValidatorFee
		return nil
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddSubnetValidatorFee
		return nil
	}

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.AddPrimaryNetworkDelegatorFee
		return nil
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) CreateChainTx(tx *txs.CreateChainTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.GetCreateBlockchainTxFee(fc.ChainTime)
		return nil
	}

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.GetCreateSubnetTxFee(fc.ChainTime)
		return nil
	}

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (*FeeCalculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil // no fees
}

func (*FeeCalculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil // no fees
}

func (fc *FeeCalculator) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TransformSubnetTxFee
		return nil
	}

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

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

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, outs, tx.Ins)
	if err != nil {
		return err
	}

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

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) BaseTx(tx *txs.BaseTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) ImportTx(tx *txs.ImportTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
	copy(ins, tx.Ins)
	copy(ins[len(tx.Ins):], tx.ImportedInputs)

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, tx.Outs, ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) ExportTx(tx *txs.ExportTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	consumedUnits, err := commonConsumedUnits(tx, fc.Credentials, outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func commonConsumedUnits(
	uTx txs.UnsignedTx,
	credentials []verify.Verifiable,
	allOuts []*avax.TransferableOutput,
	allIns []*avax.TransferableInput,
) (fees.Dimensions, error) {
	var consumedUnits fees.Dimensions

	uTxSize, err := txs.Codec.Size(txs.CodecVersion, uTx)
	if err != nil {
		return consumedUnits, fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}
	credsSize, err := txs.Codec.Size(txs.CodecVersion, credentials)
	if err != nil {
		return consumedUnits, fmt.Errorf("failed retrieving size of credentials: %w", err)
	}
	consumedUnits[fees.Bandwidth] = uint64(uTxSize + credsSize)

	var (
		insCost  uint64
		insSize  uint64
		outsSize uint64
	)
	for _, in := range allIns {
		cost, err := in.In.Cost()
		if err != nil {
			return consumedUnits, fmt.Errorf("failed retrieving cost of input %s: %w", in.ID, err)
		}
		insCost += cost

		inSize, err := txs.Codec.Size(txs.CodecVersion, in)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed retrieving size of input %s: %w", in.ID, err)
		}
		insSize += uint64(inSize)
	}

	for _, out := range allOuts {
		outSize, err := txs.Codec.Size(txs.CodecVersion, out)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed retrieving size of output %s: %w", out.ID, err)
		}
		outsSize += uint64(outSize)
	}

	consumedUnits[fees.UTXORead] = insCost + insSize   // inputs are read
	consumedUnits[fees.UTXOWrite] = insSize + outsSize // inputs are deleted, outputs are created
	return consumedUnits, nil
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
