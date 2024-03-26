// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fees"
)

func FinanceInput(feeCalc *Calculator, codec codec.Manager, input *avax.TransferableInput) (uint64, error) {
	inDimensions, err := fees.MeterInput(codec, txs.CodecVersion, input)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(inDimensions, feeCalc.TipPercentage)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}

func FinanceOutput(feeCalc *Calculator, codec codec.Manager, output *avax.TransferableOutput) (uint64, fees.Dimensions, error) {
	outDimensions, err := fees.MeterOutput(codec, txs.CodecVersion, output)
	if err != nil {
		return 0, fees.Empty, fmt.Errorf("failed calculating changeOut size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(outDimensions, feeCalc.TipPercentage)
	if err != nil {
		return 0, fees.Empty, fmt.Errorf("account for stakedOut fees: %w", err)
	}
	return addedFees, outDimensions, nil
}

func FinanceCredential(feeCalc *Calculator, codec codec.Manager, keysCount int) (uint64, error) {
	credDimensions, err := fees.MeterCredential(codec, txs.CodecVersion, keysCount)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(credDimensions, feeCalc.TipPercentage)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}
