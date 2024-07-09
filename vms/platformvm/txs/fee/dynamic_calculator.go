// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const StakerLookupCost uint64 = 1000 // equal to secp256k1fx.CostPerSignature

var (
	_ Calculator  = (*dynamicCalculator)(nil)
	_ txs.Visitor = (*staticCalculator)(nil)

	errFailedFeeCalculation = errors.New("failed fee calculation")
)

func NewDynamicCalculator(fc *fee.Calculator) Calculator {
	return &dynamicCalculator{
		fc:    fc,
		close: true,
		// credentials are set when computeFee is called
	}
}

func NewBuildingDynamicCalculator(fc *fee.Calculator) Calculator {
	return &dynamicCalculator{
		fc:    fc,
		close: false,
		// credentials are set when computeFee is called
	}
}

type dynamicCalculator struct {
	// inputs
	fc    *fee.Calculator
	cred  []verify.Verifiable
	close bool

	// outputs of visitor execution
	fee uint64
}

func (c *dynamicCalculator) CalculateFee(tx *txs.Tx) (uint64, error) {
	c.setCredentials(tx.Creds)
	c.fee = 0 // zero fee among different calculateFee invocations (unlike gas which gets cumulated)
	err := tx.Unsigned.Visit(c)
	if c.close {
		err = errors.Join(err, c.fc.DoneWithLatestTx())
	}
	return c.fee, err
}

func (c *dynamicCalculator) AddFeesFor(complexity fee.Dimensions) (uint64, error) {
	if complexity == fee.Empty {
		return 0, nil
	}
	if err := c.fc.CumulateComplexity(complexity); err != nil {
		return 0, fmt.Errorf("failed cumulating complexity: %w", err)
	}
	fee, err := c.fc.GetLatestTxFee()
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}

	extraFee := fee - c.fee
	c.fee = fee
	return extraFee, nil
}

func (c *dynamicCalculator) RemoveFeesFor(unitsToRm fee.Dimensions) (uint64, error) {
	if unitsToRm == fee.Empty {
		return 0, nil
	}
	if err := c.fc.RemoveComplexity(unitsToRm); err != nil {
		return 0, fmt.Errorf("failed removing units: %w", err)
	}
	fee, err := c.fc.GetLatestTxFee()
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errFailedFeeCalculation, err)
	}

	removedFee := c.fee - fee
	c.fee = fee
	return removedFee, nil
}

func (c *dynamicCalculator) GetFee() uint64 { return c.fee }

func (c *dynamicCalculator) ResetFee(newFee uint64) {
	c.fee = newFee
}

func (c *dynamicCalculator) GetGasPrice() fee.GasPrice { return c.fc.GetGasPrice() }

func (c *dynamicCalculator) GetBlockGas() (fee.Gas, error) { return c.fc.GetBlockGas() }

func (c *dynamicCalculator) GetExcessGas() (fee.Gas, error) { return c.fc.GetExcessGas() }

func (c *dynamicCalculator) GetGasCap() fee.Gas { return c.fc.GetGasCap() }

func (c *dynamicCalculator) setCredentials(creds []verify.Verifiable) {
	c.cred = creds
}

func (*dynamicCalculator) IsEActive() bool { return true }

func (*dynamicCalculator) AddValidatorTx(*txs.AddValidatorTx) error {
	// AddValidatorTx is banned following Durango activation
	return errFailedFeeCalculation
}

func (c *dynamicCalculator) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}
	complexity[fee.Compute] += StakerLookupCost

	_, err = c.AddFeesFor(complexity)
	return err
}

func (*dynamicCalculator) AddDelegatorTx(*txs.AddDelegatorTx) error {
	// AddDelegatorTx is banned following Durango activation
	return errFailedFeeCalculation
}

func (c *dynamicCalculator) CreateChainTx(tx *txs.CreateChainTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	c.fee = 0 // no fees
	return nil
}

func (c *dynamicCalculator) RewardValidatorTx(*txs.RewardValidatorTx) error {
	c.fee = 0 // no fees
	return nil
}

func (c *dynamicCalculator) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}
	complexity[fee.Compute] += StakerLookupCost

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	complexity, err := c.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}
	complexity[fee.Compute] += StakerLookupCost

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	complexity, err := c.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}
	complexity[fee.Compute] += StakerLookupCost

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) BaseTx(tx *txs.BaseTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) ImportTx(tx *txs.ImportTx) error {
	ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
	copy(ins, tx.Ins)
	copy(ins[len(tx.Ins):], tx.ImportedInputs)

	complexity, err := c.meterTx(tx, tx.Outs, ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) ExportTx(tx *txs.ExportTx) error {
	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	complexity, err := c.meterTx(tx, outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) meterTx(
	uTx txs.UnsignedTx,
	allOuts []*avax.TransferableOutput,
	allIns []*avax.TransferableInput,
) (fee.Dimensions, error) {
	var complexity fee.Dimensions

	uTxSize, err := txs.Codec.Size(txs.CodecVersion, uTx)
	if err != nil {
		return complexity, fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}
	complexity[fee.Bandwidth] = uint64(uTxSize)

	// meter credentials, one by one. Then account for the extra bytes needed to
	// serialize a slice of credentials (codec version bytes + slice size bytes)
	for i, cred := range c.cred {
		c, ok := cred.(*secp256k1fx.Credential)
		if !ok {
			return complexity, fmt.Errorf("don't know how to calculate complexity of %T", cred)
		}
		credDimensions, err := fee.MeterCredential(txs.Codec, txs.CodecVersion, len(c.Sigs))
		if err != nil {
			return complexity, fmt.Errorf("failed adding credential %d: %w", i, err)
		}
		complexity, err = fee.Add(complexity, credDimensions)
		if err != nil {
			return complexity, fmt.Errorf("failed adding credentials: %w", err)
		}
	}
	complexity[fee.Bandwidth] += wrappers.IntLen // length of the credentials slice
	complexity[fee.Bandwidth] += codec.VersionSize

	for _, in := range allIns {
		inputDimensions, err := fee.MeterInput(txs.Codec, txs.CodecVersion, in)
		if err != nil {
			return complexity, fmt.Errorf("failed retrieving size of inputs: %w", err)
		}
		inputDimensions[fee.Bandwidth] = 0 // inputs bandwidth is already accounted for above, so we zero it
		complexity, err = fee.Add(complexity, inputDimensions)
		if err != nil {
			return complexity, fmt.Errorf("failed adding inputs: %w", err)
		}
	}

	for _, out := range allOuts {
		outputDimensions, err := fee.MeterOutput(txs.Codec, txs.CodecVersion, out)
		if err != nil {
			return complexity, fmt.Errorf("failed retrieving size of outputs: %w", err)
		}
		outputDimensions[fee.Bandwidth] = 0 // outputs bandwidth is already accounted for above, so we zero it
		complexity, err = fee.Add(complexity, outputDimensions)
		if err != nil {
			return complexity, fmt.Errorf("failed adding outputs: %w", err)
		}
	}

	return complexity, nil
}
