// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fees"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ txs.Visitor = (*Calculator)(nil)

	errFailedFeeCalculation       = errors.New("failed fee calculation")
	errFailedComplexityCumulation = errors.New("failed cumulating complexity")
)

type Calculator struct {
	// setup, to be filled before visitor methods are called
	IsEActive bool

	// Pre E-Upgrade inputs
	Config *config.Config

	// Post E-Upgrade inputs
	FeeManager         *fees.Manager
	BlockMaxComplexity fees.Dimensions
	Codec              codec.Manager

	// TipPercentage can either be an input (e.g. when building a transaction)
	// or an output (once a transaction is verified)
	TipPercentage fees.TipPercentage

	// common inputs
	Credentials []*fxs.FxCredential

	// outputs of visitor execution
	Fee uint64
}

func (fc *Calculator) BaseTx(tx *txs.BaseTx) error {
	if !fc.IsEActive {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)
	return err
}

func (fc *Calculator) CreateAssetTx(tx *txs.CreateAssetTx) error {
	if !fc.IsEActive {
		fc.Fee = fc.Config.CreateAssetTxFee
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)
	return err
}

func (fc *Calculator) OperationTx(tx *txs.OperationTx) error {
	if !fc.IsEActive {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)
	return err
}

func (fc *Calculator) ImportTx(tx *txs.ImportTx) error {
	if !fc.IsEActive {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedIns))
	copy(ins, tx.Ins)
	copy(ins[len(tx.Ins):], tx.ImportedIns)

	complexity, err := fc.meterTx(tx, tx.Outs, ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)
	return err
}

func (fc *Calculator) ExportTx(tx *txs.ExportTx) error {
	if !fc.IsEActive {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOuts)

	complexity, err := fc.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)
	return err
}

func (fc *Calculator) meterTx(
	uTx txs.UnsignedTx,
	allOuts []*avax.TransferableOutput,
	allIns []*avax.TransferableInput,
) (fees.Dimensions, error) {
	var complexity fees.Dimensions

	uTxSize, err := fc.Codec.Size(txs.CodecVersion, uTx)
	if err != nil {
		return complexity, fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}
	complexity[fees.Bandwidth] = uint64(uTxSize)

	// meter credentials, one by one. Then account for the extra bytes needed to
	// serialize a slice of credentials (codec version bytes + slice size bytes)
	for i, cred := range fc.Credentials {
		var keysCount int
		switch c := cred.Credential.(type) {
		case *secp256k1fx.Credential:
			keysCount = len(c.Sigs)
		case *propertyfx.Credential:
			keysCount = len(c.Sigs)
		case *nftfx.Credential:
			keysCount = len(c.Sigs)
		default:
			return complexity, fmt.Errorf("don't know how to calculate complexity of %T", cred)
		}
		credDimensions, err := fees.MeterCredential(fc.Codec, txs.CodecVersion, keysCount)
		if err != nil {
			return complexity, fmt.Errorf("failed adding credential %d: %w", i, err)
		}
		complexity, err = fees.Add(complexity, credDimensions)
		if err != nil {
			return complexity, fmt.Errorf("failed adding credentials: %w", err)
		}
	}
	complexity[fees.Bandwidth] += wrappers.IntLen // length of the credentials slice
	complexity[fees.Bandwidth] += codec.CodecVersionSize

	for _, in := range allIns {
		inputDimensions, err := fees.MeterInput(fc.Codec, txs.CodecVersion, in)
		if err != nil {
			return complexity, fmt.Errorf("failed retrieving size of inputs: %w", err)
		}
		inputDimensions[fees.Bandwidth] = 0 // inputs bandwidth is already accounted for above, so we zero it
		complexity, err = fees.Add(complexity, inputDimensions)
		if err != nil {
			return complexity, fmt.Errorf("failed adding inputs: %w", err)
		}
	}

	for _, out := range allOuts {
		outputDimensions, err := fees.MeterOutput(fc.Codec, txs.CodecVersion, out)
		if err != nil {
			return complexity, fmt.Errorf("failed retrieving size of outputs: %w", err)
		}
		outputDimensions[fees.Bandwidth] = 0 // outputs bandwidth is already accounted for above, so we zero it
		complexity, err = fees.Add(complexity, outputDimensions)
		if err != nil {
			return complexity, fmt.Errorf("failed adding outputs: %w", err)
		}
	}

	return complexity, nil
}

func (fc *Calculator) AddFeesFor(complexity fees.Dimensions, tipPercentage fees.TipPercentage) (uint64, error) {
	boundBreached, dimension := fc.FeeManager.CumulateComplexity(complexity, fc.BlockMaxComplexity)
	if boundBreached {
		return 0, fmt.Errorf("%w: breached dimension %d", errFailedComplexityCumulation, dimension)
	}

	fee, err := fc.FeeManager.CalculateFee(complexity, tipPercentage)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}
	fc.Fee += fee
	return fee, nil
}

func (fc *Calculator) RemoveFeesFor(unitsToRm fees.Dimensions, tipPercentage fees.TipPercentage) (uint64, error) {
	if err := fc.FeeManager.RemoveComplexity(unitsToRm); err != nil {
		return 0, fmt.Errorf("failed removing units: %w", err)
	}

	fee, err := fc.FeeManager.CalculateFee(unitsToRm, tipPercentage)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}

	fc.Fee -= fee
	return fee, nil
}

// CalculateTipPercentage calculates and sets the tip percentage, given the fees actually paid
// and the fees required to accept the target transaction.
// [CalculateTipPercentage] requires that fc.Visit has been called for the target transaction.
func (fc *Calculator) CalculateTipPercentage(feesPaid uint64) error {
	if feesPaid < fc.Fee {
		return fmt.Errorf("fees paid are less the required fees: fees paid %v, fees required %v",
			feesPaid,
			fc.Fee,
		)
	}

	if fc.Fee == 0 {
		return nil
	}

	tip := feesPaid - fc.Fee
	fc.TipPercentage = fees.TipPercentage(tip * fees.TipDenonimator / fc.Fee)
	return fc.TipPercentage.Validate()
}
