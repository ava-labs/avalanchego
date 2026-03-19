// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/strevm/hook"
	"github.com/holiman/uint256"
)

type Import struct {
	NetworkID      uint32                    `serialize:"true" json:"networkID"`
	BlockchainID   ids.ID                    `serialize:"true" json:"blockchainID"`
	SourceChain    ids.ID                    `serialize:"true" json:"sourceChain"`
	ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
	Outs           []EVMOutput               `serialize:"true" json:"outputs"`
}

func (i *Import) InputUTXOs() set.Set[ids.ID] {
	set := set.NewSet[ids.ID](len(i.ImportedInputs))
	for _, in := range i.ImportedInputs {
		set.Add(in.InputID())
	}
	return set
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

func (i *Import) SanityCheck(ctx context.Context, snowCtx *snow.Context) error {
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

var (
	errIncorrectNumCredentials = errors.New("incorrect number of credentials")
	errFailedToFetchUTXOs      = errors.New("failed to fetch UTXOs")
	errConvertingToFxTx        = errors.New("converting to fx transaction")
	errFailedToUnmarshalUTXO   = errors.New("failed to unmarshal UTXO")
	errMismatchedAssetIDs      = errors.New("mismatched asset IDs")
	errVerifyTransferFailed    = errors.New("transfer verification failed")
)

func (i *Import) VerifyCredentials(snowCtx *snow.Context, creds []verify.Verifiable) error {
	if len(i.ImportedInputs) != len(creds) {
		return fmt.Errorf("%w: expected %d, got %d", errIncorrectNumCredentials, len(i.ImportedInputs), len(creds))
	}

	utxoIDs := make([][]byte, len(i.ImportedInputs))
	for i, in := range i.ImportedInputs {
		inputID := in.UTXOID.InputID()
		utxoIDs[i] = inputID[:]
	}

	utxoBytes, err := snowCtx.SharedMemory.Get(i.SourceChain, utxoIDs)
	if err != nil {
		return fmt.Errorf("%w from %s: %v", errFailedToFetchUTXOs, i.SourceChain, err)
	}

	fxTx, err := toFxTx(i)
	if err != nil {
		return fmt.Errorf("%w: %v", errConvertingToFxTx, err)
	}
	for i, in := range i.ImportedInputs {
		utxo := &avax.UTXO{}
		if _, err := c.Unmarshal(utxoBytes[i], utxo); err != nil {
			return fmt.Errorf("%w: %v", errFailedToUnmarshalUTXO, err)
		}
		if inAssetID, utxoAssetID := in.AssetID(), utxo.AssetID(); utxoAssetID != inAssetID {
			return fmt.Errorf("%w: input asset ID %s does not match UTXO asset ID %s", errMismatchedAssetIDs, inAssetID, utxoAssetID)
		}
		if err := fx.VerifyTransfer(fxTx, in.In, creds[i], utxo.Out); err != nil {
			return fmt.Errorf("%w: %v", errVerifyTransferFailed, err)
		}
	}
	return nil
}

var errOverflow = errors.New("amount overflow")

func (i *Import) AsOp(avaxAssetID ids.ID) (map[common.Address]hook.AccountDebit, map[common.Address]uint256.Int, error) {
	mint := make(map[common.Address]uint256.Int)
	for _, out := range i.Outs {
		if out.AssetID != avaxAssetID {
			continue
		}

		var outAmount uint256.Int
		outAmount.SetUint64(out.Amount)
		outAmount.Mul(&outAmount, x2cRate)

		amount := mint[out.Address]
		if _, overflow := amount.AddOverflow(&amount, &outAmount); overflow {
			return nil, nil, fmt.Errorf("%w: for address %s", errOverflow, out.Address)
		}
		mint[out.Address] = amount
	}
	return nil, mint, nil
}
