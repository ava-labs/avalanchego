// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fees"
)

var (
	_ txs.Visitor = (*FeeCalculator)(nil)

	errFailedFeeCalculation          = errors.New("failed fee calculation")
	errFailedConsumedUnitsCumulation = errors.New("failed cumulating consumed units")
)

type FeeCalculator struct {
	// setup, to be filled before visitor methods are called
	feeManager *fees.Manager
	Codec      codec.Manager
	Config     *config.Config
	ChainTime  time.Time

	// inputs, to be filled before visitor methods are called
	Tx *txs.Tx

	// outputs of visitor execution
	Fee uint64
}

func (fc *FeeCalculator) BaseTx(tx *txs.BaseTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits, err := commonConsumedUnits(fc.Codec, fc.Tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) CreateAssetTx(tx *txs.CreateAssetTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.CreateAssetTxFee
		return nil
	}

	consumedUnits, err := commonConsumedUnits(fc.Codec, fc.Tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func (fc *FeeCalculator) OperationTx(tx *txs.OperationTx) error {
	if !fc.Config.IsEForkActivated(fc.ChainTime) {
		fc.Fee = fc.Config.TxFee
		return nil
	}

	consumedUnits, err := commonConsumedUnits(fc.Codec, fc.Tx, tx.Outs, tx.Ins)
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

	consumedUnits, err := commonConsumedUnits(fc.Codec, fc.Tx, tx.Outs, tx.Ins)
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

	consumedUnits, err := commonConsumedUnits(fc.Codec, fc.Tx, tx.Outs, tx.Ins)
	if err != nil {
		return err
	}

	fc.Fee, err = processFees(fc.Config, fc.ChainTime, fc.feeManager, consumedUnits)
	return err
}

func commonConsumedUnits(
	codec codec.Manager,
	sTx *txs.Tx,
	allOuts []*avax.TransferableOutput,
	allIns []*avax.TransferableInput,
) (fees.Dimensions, error) {
	var consumedUnits fees.Dimensions
	consumedUnits[fees.Bandwidth] = uint64(len(sTx.Bytes()))

	// TODO ABENEGIA: consider handling imports/exports differently
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

		inSize, err := codec.Size(txs.CodecVersion, in)
		if err != nil {
			return consumedUnits, fmt.Errorf("failed retrieving size of input %s: %w", in.ID, err)
		}
		insSize += uint64(inSize)
	}

	for _, out := range allOuts {
		outSize, err := codec.Size(txs.CodecVersion, out)
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
