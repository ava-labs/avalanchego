// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fees"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func FinanceInput(feeCalc *Calculator, input *avax.TransferableInput) (uint64, error) {
	inDimensions, err := fees.MeterInput(txs.Codec, txs.CodecVersion, input)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(inDimensions, feeCalc.TipPercentage)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}

func FinanceOutput(feeCalc *Calculator, output *avax.TransferableOutput) (uint64, fees.Dimensions, error) {
	outDimensions, err := fees.MeterOutput(txs.Codec, txs.CodecVersion, output)
	if err != nil {
		return 0, fees.Empty, fmt.Errorf("failed calculating changeOut size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(outDimensions, feeCalc.TipPercentage)
	if err != nil {
		return 0, fees.Empty, fmt.Errorf("account for stakedOut fees: %w", err)
	}
	return addedFees, outDimensions, nil
}

func FinanceCredential(feeCalc *Calculator, keysCount int) (uint64, error) {
	credDimensions, err := fees.MeterCredential(txs.Codec, txs.CodecVersion, keysCount)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(credDimensions, feeCalc.TipPercentage)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}
