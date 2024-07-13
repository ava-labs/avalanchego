// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ Calculator  = (*dynamicCalculator)(nil)
	_ txs.Visitor = (*dynamicCalculator)(nil)
)

func NewDynamicCalculator(fc *fee.Calculator, codec codec.Manager) Calculator {
	return &dynamicCalculator{
		fc:         fc,
		codec:      codec,
		buildingTx: false,
		// credentials are set when computeFee is called
	}
}

func NewBuildingDynamicCalculator(fc *fee.Calculator, codec codec.Manager) Calculator {
	return &dynamicCalculator{
		fc:         fc,
		codec:      codec,
		buildingTx: true,
		// credentials are set when computeFee is called
	}
}

type dynamicCalculator struct {
	// inputs
	fc    *fee.Calculator
	codec codec.Manager
	cred  []*fxs.FxCredential

	buildingTx bool

	// outputs of visitor execution
	fee uint64
}

func (c *dynamicCalculator) CalculateFee(tx *txs.Tx) (uint64, error) {
	c.setCredentials(tx.Creds)
	c.fee = 0 // zero fee among different calculateFee invocations (unlike gas which gets cumulated)
	err := tx.Unsigned.Visit(c)
	if !c.buildingTx {
		err = errors.Join(err, c.fc.DoneWithLatestTx())
	}
	return c.fee, err
}

func (c *dynamicCalculator) AddFeesFor(complexity fee.Dimensions) (uint64, error) {
	fee, err := c.fc.AddFeesFor(complexity)
	if err != nil {
		return 0, fmt.Errorf("failed cumulating complexity: %w", err)
	}

	extraFee := fee - c.fee
	c.fee = fee
	return extraFee, nil
}

func (c *dynamicCalculator) RemoveFeesFor(unitsToRm fee.Dimensions) (uint64, error) {
	fee, err := c.fc.RemoveFeesFor(unitsToRm)
	if err != nil {
		return 0, fmt.Errorf("failed removing complexity: %w", err)
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

func (c *dynamicCalculator) GetGasCap() fee.Gas { return c.fc.GetGasCap() }

func (c *dynamicCalculator) setCredentials(creds []*fxs.FxCredential) {
	c.cred = creds
}

func (*dynamicCalculator) IsEActive() bool { return true }

func (c *dynamicCalculator) BaseTx(tx *txs.BaseTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) CreateAssetTx(tx *txs.CreateAssetTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) OperationTx(tx *txs.OperationTx) error {
	complexity, err := c.meterTx(tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) ImportTx(tx *txs.ImportTx) error {
	ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedIns))
	copy(ins, tx.Ins)
	copy(ins[len(tx.Ins):], tx.ImportedIns)

	complexity, err := c.meterTx(tx, tx.Outs, ins)
	if err != nil {
		return err
	}

	_, err = c.AddFeesFor(complexity)
	return err
}

func (c *dynamicCalculator) ExportTx(tx *txs.ExportTx) error {
	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOuts)

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

	uTxSize, err := c.codec.Size(txs.CodecVersion, uTx)
	if err != nil {
		return complexity, fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}
	complexity[fee.Bandwidth] = uint64(uTxSize)

	// meter credentials, one by one. Then account for the extra bytes needed to
	// serialize a slice of credentials (codec version bytes + slice size bytes)
	for i, cred := range c.cred {
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
		credDimensions, err := fee.MeterCredential(c.codec, txs.CodecVersion, keysCount)
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
		inputDimensions, err := fee.MeterInput(c.codec, txs.CodecVersion, in)
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
		outputDimensions, err := fee.MeterOutput(c.codec, txs.CodecVersion, out)
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
