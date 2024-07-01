// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"

	commonfee "github.com/ava-labs/avalanchego/vms/components/fee"
)

func FinanceInput(feeCalc *fee.Calculator, input *avax.TransferableInput) (uint64, error) {
	if !feeCalc.IsEActive() {
		return 0, nil // pre E-upgrade we have a fixed fee regardless how complex the input is
	}

	inDimensions, err := commonfee.MeterInput(txs.Codec, txs.CodecVersion, input)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(inDimensions)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}

func FinanceOutput(feeCalc *fee.Calculator, output *avax.TransferableOutput) (uint64, commonfee.Dimensions, error) {
	if !feeCalc.IsEActive() {
		return 0, commonfee.Empty, nil // pre E-upgrade we have a fixed fee regardless how complex the output is
	}

	outDimensions, err := commonfee.MeterOutput(txs.Codec, txs.CodecVersion, output)
	if err != nil {
		return 0, commonfee.Empty, fmt.Errorf("failed calculating changeOut size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(outDimensions)
	if err != nil {
		return 0, commonfee.Empty, fmt.Errorf("account for stakedOut fees: %w", err)
	}
	return addedFees, outDimensions, nil
}

func FinanceCredential(feeCalc *fee.Calculator, keysCount int) (uint64, error) {
	if !feeCalc.IsEActive() {
		return 0, nil // pre E-upgrade we have a fixed fee regardless how complex the credentials are
	}

	credDimensions, err := commonfee.MeterCredential(txs.Codec, txs.CodecVersion, keysCount)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(credDimensions)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}
