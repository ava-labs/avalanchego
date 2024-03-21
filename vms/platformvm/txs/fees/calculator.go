// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fees"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ txs.Visitor = (*Calculator)(nil)

	errFailedFeeCalculation          = errors.New("failed fee calculation")
	errFailedConsumedUnitsCumulation = errors.New("failed cumulating consumed units")
)

type Calculator struct {
	// setup
	IsEUpgradeActive bool

	// Pre E-fork inputs
	Config    *config.Config
	ChainTime time.Time

	// Post E-fork inputs
	FeeManager         *fees.Manager
	BlockMaxComplexity fees.Dimensions

	// common inputs
	Credentials []verify.Verifiable

	// outputs of visitor execution
	Fee uint64
}

func (fc *Calculator) AddValidatorTx(*txs.AddValidatorTx) error {
	// AddValidatorTx is banned following Durango activation, so we
	// only return the pre EUpgrade fee here
	fc.Fee = fc.Config.AddPrimaryNetworkValidatorFee
	return nil
}

func (fc *Calculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	// AddValidatorTx is banned following Durango activation, so we
	// only return the pre EUpgrade fee here
	fc.Fee = fc.Config.AddPrimaryNetworkDelegatorFee
	return nil
}

func (*Calculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil // no fees
}

func (*Calculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil // no fees
}

func (fc *Calculator) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	if !fc.IsEUpgradeActive {
		fc.Fee = fc.Config.AddSubnetValidatorFee
		return nil
	}

	consumedUnits, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) CreateChainTx(tx *txs.CreateChainTx) error {
	if !fc.IsEUpgradeActive {
		fc.Fee = fc.Config.GetCreateBlockchainTxFee(fc.ChainTime)
		return nil
	}

	consumedUnits, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	if !fc.IsEUpgradeActive {
		fc.Fee = fc.Config.GetCreateSubnetTxFee(fc.ChainTime)
		return nil
	}

	consumedUnits, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	if !fc.IsEUpgradeActive {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	if !fc.IsEUpgradeActive {
		fc.Fee = fc.Config.TransformSubnetTxFee
		return nil
	}

	consumedUnits, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	if !fc.IsEUpgradeActive {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if !fc.IsEUpgradeActive {
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

	consumedUnits, err := fc.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if !fc.IsEUpgradeActive {
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

	consumedUnits, err := fc.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) BaseTx(tx *txs.BaseTx) error {
	if !fc.IsEUpgradeActive {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) ImportTx(tx *txs.ImportTx) error {
	if !fc.IsEUpgradeActive {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
	copy(ins, tx.Ins)
	copy(ins[len(tx.Ins):], tx.ImportedInputs)

	consumedUnits, err := fc.meterTx(tx, tx.Outs, ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) ExportTx(tx *txs.ExportTx) error {
	if !fc.IsEUpgradeActive {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	consumedUnits, err := fc.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(consumedUnits)
	return err
}

func (fc *Calculator) meterTx(
	uTx txs.UnsignedTx,
	allOuts []*avax.TransferableOutput,
	allIns []*avax.TransferableInput,
) (fees.Dimensions, error) {
	var consumedUnits fees.Dimensions

	uTxSize, err := txs.Codec.Size(txs.CodecVersion, uTx)
	if err != nil {
		return consumedUnits, fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}
	consumedUnits[fees.Bandwidth] = uint64(uTxSize)

	// meter credentials, one by one. Then account for the extra bytes needed to
	// serialize a slice of credentials (codec version bytes + slice size bytes)
	for i, cred := range fc.Credentials {
		c, ok := cred.(*secp256k1fx.Credential)
		if !ok {
			return consumedUnits, fmt.Errorf("don't know how to calculate complexity of %T", cred)
		}
		credDimensions, err := fees.MeterCredential(txs.Codec, txs.CodecVersion, len(c.Sigs))
		if err != nil {
			return consumedUnits, fmt.Errorf("failed adding credential %d: %w", i, err)
		}
		consumedUnits, err = fees.Add(consumedUnits, credDimensions)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed adding credentials: %w", err)
		}
	}
	consumedUnits[fees.Bandwidth] += wrappers.IntLen // length of the credentials slice
	consumedUnits[fees.Bandwidth] += codec.CodecVersionSize

	for _, in := range allIns {
		inputDimensions, err := fees.MeterInput(txs.Codec, txs.CodecVersion, in)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed retrieving size of inputs: %w", err)
		}
		inputDimensions[fees.Bandwidth] = 0 // inputs bandwidth is already accounted for above, so we zero it
		consumedUnits, err = fees.Add(consumedUnits, inputDimensions)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed adding inputs: %w", err)
		}
	}

	for _, out := range allOuts {
		outputDimensions, err := fees.MeterOutput(txs.Codec, txs.CodecVersion, out)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed retrieving size of outputs: %w", err)
		}
		outputDimensions[fees.Bandwidth] = 0 // outputs bandwidth is already accounted for above, so we zero it
		consumedUnits, err = fees.Add(consumedUnits, outputDimensions)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed adding outputs: %w", err)
		}
	}

	return consumedUnits, nil
}

func (fc *Calculator) AddFeesFor(consumedUnits fees.Dimensions) (uint64, error) {
	boundBreached, dimension := fc.FeeManager.CumulateUnits(consumedUnits, fc.BlockMaxComplexity)
	if boundBreached {
		return 0, fmt.Errorf("%w: breached dimension %d", errFailedConsumedUnitsCumulation, dimension)
	}

	fee, err := fc.FeeManager.CalculateFee(consumedUnits)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}

	fc.Fee += fee
	return fee, nil
}

func (fc *Calculator) RemoveFeesFor(unitsToRm fees.Dimensions) (uint64, error) {
	if err := fc.FeeManager.RemoveUnits(unitsToRm); err != nil {
		return 0, fmt.Errorf("failed removing units: %w", err)
	}

	fee, err := fc.FeeManager.CalculateFee(unitsToRm)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}

	fc.Fee -= fee
	return fee, nil
}
