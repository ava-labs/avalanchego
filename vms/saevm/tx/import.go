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

type Import struct {
	NetworkID      uint32                    `serialize:"true" json:"networkID"`
	BlockchainID   ids.ID                    `serialize:"true" json:"blockchainID"`
	SourceChain    ids.ID                    `serialize:"true" json:"sourceChain"`
	ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
	Outs           []EVMOutput               `serialize:"true" json:"outputs"`
}

func (i *Import) Burned(assetID ids.ID) (uint64, error) {
	var (
		output uint64
		err    error
	)
	for _, out := range i.Outs {
		if out.AssetID == assetID {
			output, err = math.Add(output, out.Amount)
			if err != nil {
				return 0, err
			}
		}
	}
	var input uint64
	for _, in := range i.ImportedInputs {
		if in.AssetID() == assetID {
			input, err = math.Add(input, in.In.Amount())
			if err != nil {
				return 0, err
			}
		}
	}
	return math.Sub(input, output)
}

var (
	errWrongNetworkID         = errors.New("wrong network ID")
	errWrongChainID           = errors.New("wrong chain ID")
	errNoInputs               = errors.New("no inputs")
	errNoOutputs              = errors.New("no outputs")
	errNotSameSubnet          = errors.New("not same subnet")
	errInvalidInput           = errors.New("invalid input")
	errNonAVAXInput           = errors.New("input contains non-AVAX")
	errInvalidOutput          = errors.New("invalid output")
	errNonAVAXOutput          = errors.New("output contains non-AVAX")
	errFlowCheckFailed        = errors.New("flow check failed")
	errInputsNotSortedUnique  = errors.New("inputs not sorted and unique")
	errOutputsNotSortedUnique = errors.New("outputs not sorted and unique")
)

func (i *Import) Verify(ctx context.Context, snowCtx *snow.Context) error {
	switch {
	case i.NetworkID != snowCtx.NetworkID:
		return fmt.Errorf("%w: expected %d, got %d", errWrongNetworkID, snowCtx.NetworkID, i.NetworkID)
	case i.BlockchainID != snowCtx.ChainID:
		return fmt.Errorf("%w: expected %d, got %d", errWrongChainID, snowCtx.ChainID, i.BlockchainID)
	case len(i.ImportedInputs) == 0:
		return errNoInputs
	case len(i.Outs) == 0:
		return errNoOutputs
	}

	if err := verify.SameSubnet(ctx, snowCtx, i.SourceChain); err != nil {
		return fmt.Errorf("%w: %v", errNotSameSubnet, err)
	}

	fc := avax.NewFlowChecker()
	for i, in := range i.ImportedInputs {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %v", errInvalidInput, i, err)
		}
		if assetID := in.AssetID(); assetID != snowCtx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXInput, i, snowCtx.AVAXAssetID, assetID)
		}
		fc.Consume(snowCtx.AVAXAssetID, in.In.Amount())
	}
	for i, out := range i.Outs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %v", errInvalidOutput, i, err)
		}
		if out.AssetID != snowCtx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXOutput, i, snowCtx.AVAXAssetID, out.AssetID)
		}
		fc.Produce(snowCtx.AVAXAssetID, out.Amount)
	}
	if err := fc.Verify(); err != nil {
		return fmt.Errorf("%w: %v", errFlowCheckFailed, err)
	}

	if !utils.IsSortedAndUnique(i.ImportedInputs) {
		return errInputsNotSortedUnique
	}
	if !utils.IsSortedAndUnique(i.Outs) {
		return errOutputsNotSortedUnique
	}

	return nil
}
