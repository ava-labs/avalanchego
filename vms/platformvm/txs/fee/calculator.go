// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ txs.Visitor = (*Calculator)(nil)

	errFailedFeeCalculation       = errors.New("failed fee calculation")
	errFailedComplexityCumulation = errors.New("failed cumulating complexity")
)

type Calculator struct {
	// setup
	isEActive bool

	// Pre E-fork inputs
	upgrades  upgrade.Times
	staticCfg StaticConfig
	chainTime time.Time

	// Post E-fork inputs
	feeManager         *fees.Manager
	blockMaxComplexity fees.Dimensions
	credentials        []verify.Verifiable

	// outputs of visitor execution
	Fee uint64
}

func NewStaticCalculator(cfg StaticConfig, ut upgrade.Times, chainTime time.Time) *Calculator {
	return &Calculator{
		upgrades:  ut,
		staticCfg: cfg,
		chainTime: chainTime,
	}
}

// NewDynamicCalculator must be used post E upgrade activation
func NewDynamicCalculator(
	cfg StaticConfig,
	feeManager *fees.Manager,
	blockMaxComplexity fees.Dimensions,
	creds []verify.Verifiable,
) *Calculator {
	return &Calculator{
		isEActive:          true,
		staticCfg:          cfg,
		feeManager:         feeManager,
		blockMaxComplexity: blockMaxComplexity,
		credentials:        creds,
	}
}

func (fc *Calculator) AddValidatorTx(*txs.AddValidatorTx) error {
	// AddValidatorTx is banned following Durango activation, so we
	// only return the pre EUpgrade fee here
	fc.Fee = fc.staticCfg.AddPrimaryNetworkValidatorFee
	return nil
}

func (fc *Calculator) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	if !fc.isEActive {
		fc.Fee = fc.staticCfg.AddSubnetValidatorFee
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	// AddValidatorTx is banned following Durango activation, so we
	// only return the pre EUpgrade fee here
	fc.Fee = fc.staticCfg.AddPrimaryNetworkDelegatorFee
	return nil
}

func (fc *Calculator) CreateChainTx(tx *txs.CreateChainTx) error {
	if !fc.isEActive {
		fc.Fee = fc.staticCfg.GetCreateBlockchainTxFee(fc.upgrades, fc.chainTime)
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	if !fc.isEActive {
		fc.Fee = fc.staticCfg.GetCreateSubnetTxFee(fc.upgrades, fc.chainTime)
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (*Calculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil // no fees
}

func (*Calculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil // no fees
}

func (fc *Calculator) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	if !fc.isEActive {
		fc.Fee = fc.staticCfg.TxFee
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	if !fc.isEActive {
		fc.Fee = fc.staticCfg.TransformSubnetTxFee
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	if !fc.isEActive {
		fc.Fee = fc.staticCfg.TxFee
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if !fc.isEActive {
		if tx.Subnet != constants.PrimaryNetworkID {
			fc.Fee = fc.staticCfg.AddSubnetValidatorFee
		} else {
			fc.Fee = fc.staticCfg.AddPrimaryNetworkValidatorFee
		}
		return nil
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	complexity, err := fc.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if !fc.isEActive {
		if tx.Subnet != constants.PrimaryNetworkID {
			fc.Fee = fc.staticCfg.AddSubnetDelegatorFee
		} else {
			fc.Fee = fc.staticCfg.AddPrimaryNetworkDelegatorFee
		}
		return nil
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	complexity, err := fc.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) BaseTx(tx *txs.BaseTx) error {
	if !fc.isEActive {
		fc.Fee = fc.staticCfg.TxFee
		return nil
	}

	complexity, err := fc.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) ImportTx(tx *txs.ImportTx) error {
	if !fc.isEActive {
		fc.Fee = fc.staticCfg.TxFee
		return nil
	}

	ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
	copy(ins, tx.Ins)
	copy(ins[len(tx.Ins):], tx.ImportedInputs)

	complexity, err := fc.meterTx(tx, tx.Outs, ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) ExportTx(tx *txs.ExportTx) error {
	if !fc.isEActive {
		fc.Fee = fc.staticCfg.TxFee
		return nil
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	complexity, err := fc.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = fc.AddFeesFor(complexity)
	return err
}

func (fc *Calculator) meterTx(
	uTx txs.UnsignedTx,
	allOuts []*avax.TransferableOutput,
	allIns []*avax.TransferableInput,
) (fees.Dimensions, error) {
	var complexity fees.Dimensions

	uTxSize, err := txs.Codec.Size(txs.CodecVersion, uTx)
	if err != nil {
		return complexity, fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}
	complexity[fees.Bandwidth] = uint64(uTxSize)

	// meter credentials, one by one. Then account for the extra bytes needed to
	// serialize a slice of credentials (codec version bytes + slice size bytes)
	for i, cred := range fc.credentials {
		c, ok := cred.(*secp256k1fx.Credential)
		if !ok {
			return complexity, fmt.Errorf("don't know how to calculate complexity of %T", cred)
		}
		credDimensions, err := fees.MeterCredential(txs.Codec, txs.CodecVersion, len(c.Sigs))
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
		inputDimensions, err := fees.MeterInput(txs.Codec, txs.CodecVersion, in)
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
		outputDimensions, err := fees.MeterOutput(txs.Codec, txs.CodecVersion, out)
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

func (fc *Calculator) AddFeesFor(complexity fees.Dimensions) (uint64, error) {
	if fc.feeManager == nil || complexity == fees.Empty {
		return 0, nil
	}

	boundBreached, dimension := fc.feeManager.CumulateComplexity(complexity, fc.blockMaxComplexity)
	if boundBreached {
		return 0, fmt.Errorf("%w: breached dimension %d", errFailedComplexityCumulation, dimension)
	}

	fee, err := fc.feeManager.CalculateFee(complexity)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}

	fc.Fee += fee
	return fee, nil
}

func (fc *Calculator) RemoveFeesFor(unitsToRm fees.Dimensions) (uint64, error) {
	if fc.feeManager == nil || unitsToRm == fees.Empty {
		return 0, nil
	}

	if err := fc.feeManager.RemoveComplexity(unitsToRm); err != nil {
		return 0, fmt.Errorf("failed removing units: %w", err)
	}

	fee, err := fc.feeManager.CalculateFee(unitsToRm)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}

	fc.Fee -= fee
	return fee, nil
}
