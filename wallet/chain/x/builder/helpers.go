// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	commonfee "github.com/ava-labs/avalanchego/vms/components/fee"
)

func financeInput(feeCalc fee.Calculator, codec codec.Manager, input *avax.TransferableInput) (uint64, error) {
	if !feeCalc.IsEActive() {
		return 0, nil // pre E-upgrade we have a fixed fee regardless how complex the input is
	}

	inDimensions, err := commonfee.MeterInput(codec, txs.CodecVersion, input)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(inDimensions)
	if err != nil {
		return 0, fmt.Errorf("account for input fee: %w", err)
	}
	return addedFees, nil
}

func financeOutput(feeCalc fee.Calculator, codec codec.Manager, output *avax.TransferableOutput) (uint64, commonfee.Dimensions, error) {
	if !feeCalc.IsEActive() {
		return 0, commonfee.Empty, nil // pre E-upgrade we have a fixed fee regardless how complex the output is
	}

	outDimensions, err := commonfee.MeterOutput(codec, txs.CodecVersion, output)
	if err != nil {
		return 0, commonfee.Empty, fmt.Errorf("failed calculating changeOut size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(outDimensions)
	if err != nil {
		return 0, commonfee.Empty, fmt.Errorf("account for stakedOut fee: %w", err)
	}
	return addedFees, outDimensions, nil
}

func financeCredential(feeCalc fee.Calculator, codec codec.Manager, keysCount int) (uint64, error) {
	if !feeCalc.IsEActive() {
		return 0, nil // pre E-upgrade we have a fixed fee regardless how complex the credentials are
	}

	credDimensions, err := commonfee.MeterCredential(codec, txs.CodecVersion, keysCount)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(credDimensions)
	if err != nil {
		return 0, fmt.Errorf("account for input fee: %w", err)
	}
	return addedFees, nil
}
