// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

type Export struct {
	NetworkID        uint32                     `serialize:"true" json:"networkID"`
	BlockchainID     ids.ID                     `serialize:"true" json:"blockchainID"`
	DestinationChain ids.ID                     `serialize:"true" json:"destinationChain"`
	Ins              []EVMInput                 `serialize:"true" json:"inputs"`
	ExportedOutputs  []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

func (e *Export) Burned(assetID ids.ID) (uint64, error) {
	var (
		output uint64
		err    error
	)
	for _, out := range e.ExportedOutputs {
		if out.AssetID() == assetID {
			output, err = math.Add(output, out.Out.Amount())
			if err != nil {
				return 0, err
			}
		}
	}
	var input uint64
	for _, in := range e.Ins {
		if in.AssetID == assetID {
			input, err = math.Add(input, in.Amount)
			if err != nil {
				return 0, err
			}
		}
	}
	return math.Sub(input, output)
}

var errOutputsNotSorted = errors.New("outputs not sorted")

func (e *Export) Verify(ctx context.Context, snowCtx *snow.Context) error {
	switch {
	case e.NetworkID != snowCtx.NetworkID:
		return fmt.Errorf("%w: expected %d, got %d", errWrongNetworkID, snowCtx.NetworkID, e.NetworkID)
	case e.BlockchainID != snowCtx.ChainID:
		return fmt.Errorf("%w: expected %d, got %d", errWrongChainID, snowCtx.ChainID, e.BlockchainID)
	case len(e.Ins) == 0:
		return errNoInputs
	case len(e.ExportedOutputs) == 0:
		return errNoOutputs
	}

	if err := verify.SameSubnet(ctx, snowCtx, e.DestinationChain); err != nil {
		return fmt.Errorf("%w: %v", errNotSameSubnet, err)
	}

	fc := avax.NewFlowChecker()
	for i, in := range e.Ins {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %v", errInvalidInput, i, err)
		}
		if in.AssetID != snowCtx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXInput, i, snowCtx.AVAXAssetID, in.AssetID)
		}
		fc.Consume(snowCtx.AVAXAssetID, in.Amount)
	}
	for i, out := range e.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %v", errInvalidOutput, i, err)
		}
		if assetID := out.AssetID(); assetID != snowCtx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXOutput, i, snowCtx.AVAXAssetID, assetID)
		}
		fc.Produce(snowCtx.AVAXAssetID, out.Out.Amount())
	}
	if err := fc.Verify(); err != nil {
		return fmt.Errorf("%w: %v", errFlowCheckFailed, err)
	}

	if !utils.IsSortedAndUnique(e.Ins) {
		return errInputsNotSortedUnique
	}
	// TODO: Should this be unique?
	if !avax.IsSortedTransferableOutputs(e.ExportedOutputs, c) {
		return errOutputsNotSorted
	}

	return nil
}
