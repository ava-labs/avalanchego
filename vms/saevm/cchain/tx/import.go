// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Import is the unsigned component of a transaction that transfers assets from
// either the P-Chain or the X-Chain to the C-Chain. It consumes UTXOs in the
// shared memory between the C-Chain and the source chain and increases balances
// in the C-Chain state.
type Import struct {
	NetworkID      uint32                    `serialize:"true" json:"networkID"`
	BlockchainID   ids.ID                    `serialize:"true" json:"blockchainID"`
	SourceChain    ids.ID                    `serialize:"true" json:"sourceChain"`
	ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
	Outs           []Output                  `serialize:"true" json:"outputs"`
}

func (i *Import) InputUTXOs() set.Set[ids.ID] {
	set := set.NewSet[ids.ID](len(i.ImportedInputs))
	for _, in := range i.ImportedInputs {
		set.Add(in.InputID())
	}
	return set
}

// Output specifies an account on the C-Chain whose balance of the specified
// asset should be increased.
//
// If the AssetID is AVAX, the amount will be scaled up to account for the EVM's
// higher denomination.
type Output struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
}

func (o Output) Compare(oo Output) int {
	if c := o.Address.Cmp(oo.Address); c != 0 {
		return c
	}
	return o.AssetID.Compare(oo.AssetID)
}

func (o Output) Verify() error {
	if o.Amount == 0 {
		return errZeroAmount
	}
	return nil
}

func (i *Import) burned(assetID ids.ID) (uint64, error) {
	var (
		burned uint64
		err    error
	)
	for _, in := range i.ImportedInputs {
		if in.Asset.ID == assetID {
			burned, err = math.Add(burned, in.In.Amount())
			if err != nil {
				return 0, err
			}
		}
	}
	for _, out := range i.Outs {
		if out.AssetID == assetID {
			burned, err = math.Sub(burned, out.Amount)
			if err != nil {
				return 0, err
			}
		}
	}
	return burned, nil
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
		return fmt.Errorf("%w: %w", errNotSameSubnet, err)
	}

	fc := avax.NewFlowChecker()
	for i, in := range i.ImportedInputs {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %w", errInvalidInput, i, err)
		}
		if assetID := in.AssetID(); assetID != snowCtx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXInput, i, snowCtx.AVAXAssetID, assetID)
		}
		fc.Consume(snowCtx.AVAXAssetID, in.In.Amount())
	}
	for i, out := range i.Outs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %w", errInvalidOutput, i, err)
		}
		if out.AssetID != snowCtx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXOutput, i, snowCtx.AVAXAssetID, out.AssetID)
		}
		fc.Produce(snowCtx.AVAXAssetID, out.Amount)
	}
	if err := fc.Verify(); err != nil {
		return fmt.Errorf("%w: %w", errFlowCheckFailed, err)
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

func (i *Import) verifyCredentials(sm atomic.SharedMemory, creds []Credential) error {
	if len(i.ImportedInputs) != len(creds) {
		return fmt.Errorf("%w: expected %d, got %d", errIncorrectNumCredentials, len(i.ImportedInputs), len(creds))
	}

	utxoIDs := make([][]byte, len(i.ImportedInputs))
	for i, in := range i.ImportedInputs {
		inputID := in.UTXOID.InputID()
		utxoIDs[i] = inputID[:]
	}

	utxoBytes, err := sm.Get(i.SourceChain, utxoIDs)
	if err != nil {
		return fmt.Errorf("%w from %s: %w", errFailedToFetchUTXOs, i.SourceChain, err)
	}

	fxTx, err := toFxTx(i)
	if err != nil {
		return fmt.Errorf("%w: %w", errConvertingToFxTx, err)
	}
	for i, in := range i.ImportedInputs {
		utxo := &avax.UTXO{}
		if _, err := c.Unmarshal(utxoBytes[i], utxo); err != nil {
			return fmt.Errorf("%w: %w", errFailedToUnmarshalUTXO, err)
		}
		if inAssetID, utxoAssetID := in.AssetID(), utxo.AssetID(); utxoAssetID != inAssetID {
			return fmt.Errorf("%w: input asset ID %s does not match UTXO asset ID %s", errMismatchedAssetIDs, inAssetID, utxoAssetID)
		}
		if err := fx.VerifyTransfer(fxTx, in.In, creds[i], utxo.Out); err != nil {
			return fmt.Errorf("%w: %w", errVerifyTransferFailed, err)
		}
	}
	return nil
}

func (*Import) VerifyState(ids.ID, libevm.StateReader) error {
	return nil
}

var errUnexpectedInputType = errors.New("unexpected input type")

func (i *Import) numSigs() (uint64, error) {
	var n uint64
	for _, in := range i.ImportedInputs {
		input, ok := in.In.(*secp256k1fx.TransferInput)
		if !ok {
			return 0, fmt.Errorf("%w: got %T ; want %T", errUnexpectedInputType, in.In, input)
		}
		n += uint64(len(input.SigIndices))
	}
	return n, nil
}

var errOverflow = errors.New("amount overflow")

func (i *Import) asOp(avaxAssetID ids.ID) (op, error) {
	mint := make(map[common.Address]uint256.Int, len(i.Outs))
	for _, out := range i.Outs {
		if out.AssetID != avaxAssetID {
			continue
		}

		amount := scaleAVAX(out.Amount)
		total := mint[out.Address]
		if _, overflow := total.AddOverflow(&total, &amount); overflow {
			return op{}, fmt.Errorf("%w: for address %s", errOverflow, out.Address)
		}
		mint[out.Address] = total
	}
	return op{
		mint: mint,
	}, nil
}

func (i *Import) atomicRequests(ids.ID) (ids.ID, *atomic.Requests, error) {
	utxoIDs := make([][]byte, len(i.ImportedInputs))
	for j, in := range i.ImportedInputs {
		inputID := in.InputID()
		utxoIDs[j] = inputID[:]
	}
	return i.SourceChain, &atomic.Requests{RemoveRequests: utxoIDs}, nil
}

func (i *Import) TransferNonAVAX(avaxAssetID ids.ID, statedb *extstate.StateDB) error {
	for _, out := range i.Outs {
		if out.AssetID == avaxAssetID {
			continue
		}

		coinID := common.Hash(out.AssetID)
		amount := new(big.Int).SetUint64(out.Amount)
		statedb.AddBalanceMultiCoin(out.Address, coinID, amount)
	}
	return nil
}
