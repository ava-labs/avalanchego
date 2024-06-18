// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func FinanceInput(feeCalc *Calculator, input *avax.TransferableInput) (uint64, error) {
	if !feeCalc.c.isEActive {
		return 0, nil // pre E-upgrade we have a fixed fee regardless how complex the input is
	}

	inDimensions, err := fee.MeterInput(txs.Codec, txs.CodecVersion, input)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(inDimensions)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}

func FinanceOutput(feeCalc *Calculator, output *avax.TransferableOutput) (uint64, fee.Dimensions, error) {
	if !feeCalc.c.isEActive {
		return 0, fee.Empty, nil // pre E-upgrade we have a fixed fee regardless how complex the output is
	}

	outDimensions, err := fee.MeterOutput(txs.Codec, txs.CodecVersion, output)
	if err != nil {
		return 0, fee.Empty, fmt.Errorf("failed calculating changeOut size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(outDimensions)
	if err != nil {
		return 0, fee.Empty, fmt.Errorf("account for stakedOut fees: %w", err)
	}
	return addedFees, outDimensions, nil
}

func FinanceCredential(feeCalc *Calculator, keysCount int) (uint64, error) {
	if !feeCalc.c.isEActive {
		return 0, nil // pre E-upgrade we have a fixed fee regardless how complex the credentials are
	}

	credDimensions, err := fee.MeterCredential(txs.Codec, txs.CodecVersion, keysCount)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(credDimensions)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}
