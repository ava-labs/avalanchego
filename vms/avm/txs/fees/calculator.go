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
	isEActive bool

	// Pre E-Upgrade inputs
	config *config.Config

	// Post E-Upgrade inputs
	feeManager         *fees.Manager
	blockMaxComplexity fees.Dimensions
	codec              codec.Manager
	credentials        []*fxs.FxCredential

	// TipPercentage can either be an input (e.g. when building a transaction)
	// or an output (once a transaction is verified)
	TipPercentage fees.TipPercentage

	// outputs of visitor execution
	Fee uint64
}

// NewStaticCalculator must be used pre E upgrade activation
func NewStaticCalculator(cfg *config.Config, creds []*fxs.FxCredential) *Calculator {
	return &Calculator{
		config: cfg,

		// TEMP TO TUNE PARAMETERS
		feeManager:         fees.NewManager(fees.Empty),
		blockMaxComplexity: fees.Max,
		credentials:        creds,
	}
}

// NewDynamicCalculator must be used post E upgrade activation
func NewDynamicCalculator(
	codec codec.Manager,
	feeManager *fees.Manager,
	blockMaxComplexity fees.Dimensions,
	creds []*fxs.FxCredential,
) *Calculator {
	return &Calculator{
		isEActive:          true,
		feeManager:         feeManager,
		blockMaxComplexity: blockMaxComplexity,
		codec:              codec,
		credentials:        creds,
	}
}

func (fc *Calculator) BaseTx(tx *txs.BaseTx) error {
	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)

	// TEMP TO TUNE PARAMETERS
	if !fc.isEActive {
		fc.Fee = fc.config.TxFee
	}
	return err
}

func (fc *Calculator) CreateAssetTx(tx *txs.CreateAssetTx) error {
	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)

	// TEMP TO TUNE PARAMETERS
	if !fc.isEActive {
		fc.Fee = fc.config.CreateAssetTxFee
	}
	return err
}

func (fc *Calculator) OperationTx(tx *txs.OperationTx) error {
	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)

	// TEMP TO TUNE PARAMETERS
	if !fc.isEActive {
		fc.Fee = fc.config.TxFee
	}
	return err
}

func (fc *Calculator) ImportTx(tx *txs.ImportTx) error {
	ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedIns))
	copy(ins, tx.Ins)
	copy(ins[len(tx.Ins):], tx.ImportedIns)

	complexity, err := fc.meterTx(tx, tx.Outs, ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)

	// TEMP TO TUNE PARAMETERS
	if !fc.isEActive {
		fc.Fee = fc.config.TxFee
	}
	return err
}

func (fc *Calculator) ExportTx(tx *txs.ExportTx) error {
	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOuts)

	complexity, err := fc.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity, fc.TipPercentage)

	// TEMP TO TUNE PARAMETERS
	if !fc.isEActive {
		fc.Fee = fc.config.TxFee
	}
	return err
}

func (fc *Calculator) meterTx(
	uTx txs.UnsignedTx,
	allOuts []*avax.TransferableOutput,
	allIns []*avax.TransferableInput,
) (fees.Dimensions, error) {
	var complexity fees.Dimensions

	uTxSize, err := fc.codec.Size(txs.CodecVersion, uTx)
	if err != nil {
		return complexity, fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}
	complexity[fees.Bandwidth] = uint64(uTxSize)

	// meter credentials, one by one. Then account for the extra bytes needed to
	// serialize a slice of credentials (codec version bytes + slice size bytes)
	for i, cred := range fc.credentials {
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
		credDimensions, err := fees.MeterCredential(fc.codec, txs.CodecVersion, keysCount)
		if err != nil {
			return complexity, fmt.Errorf("failed adding credential %d: %w", i, err)
		}
		complexity, err = fees.Add(complexity, credDimensions)
		if err != nil {
			return complexity, fmt.Errorf("failed adding credentials: %w", err)
		}
	}
	complexity[fees.Bandwidth] += wrappers.IntLen // length of the credentials slice
	complexity[fees.Bandwidth] += codec.VersionSize

	for _, in := range allIns {
		inputDimensions, err := fees.MeterInput(fc.codec, txs.CodecVersion, in)
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
		outputDimensions, err := fees.MeterOutput(fc.codec, txs.CodecVersion, out)
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
	if fc.feeManager == nil || complexity == fees.Empty {
		return 0, nil
	}

	boundBreached, dimension := fc.feeManager.CumulateComplexity(complexity, fc.blockMaxComplexity)
	if boundBreached {
		return 0, fmt.Errorf("%w: breached dimension %d", errFailedComplexityCumulation, dimension)
	}

	fee, err := fc.feeManager.CalculateFee(complexity, tipPercentage)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}
	fc.Fee += fee
	return fee, nil
}

func (fc *Calculator) RemoveFeesFor(unitsToRm fees.Dimensions, tipPercentage fees.TipPercentage) (uint64, error) {
	if fc.feeManager == nil || unitsToRm == fees.Empty {
		return 0, nil
	}

	if err := fc.feeManager.RemoveComplexity(unitsToRm); err != nil {
		return 0, fmt.Errorf("failed removing units: %w", err)
	}

	fee, err := fc.feeManager.CalculateFee(unitsToRm, tipPercentage)
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
