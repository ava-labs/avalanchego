// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/holiman/uint256"

	// Imported for [atomic.UnsignedImportTx.Burned] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

var _ Unsigned = (*Import)(nil)

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

// Compare orders [Output] values by [Output.Address] and [Output.AssetID].
func (o Output) Compare(other Output) int {
	if c := o.Address.Cmp(other.Address); c != 0 {
		return c
	}
	return o.AssetID.Compare(other.AssetID)
}

// InputIDs returns the UTXOIDs consumed by this transaction.
func (i *Import) InputIDs() set.Set[ids.ID] {
	s := set.NewSet[ids.ID](len(i.ImportedInputs))
	for _, in := range i.ImportedInputs {
		s.Add(in.InputID())
	}
	return s
}

// Like [atomic.UnsignedImportTx.Burned], burned will error if the sum of the
// inputs exceeds MaxUint64, even if the total amount burned could be
// represented as a uint64.
//
// Because the total supply of AVAX fits in a uint64, this doesn't matter in
// practice and allows for easier fuzzing.
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

var errOutputsNotSortedUnique = errors.New("outputs not sorted and unique")

// SanityCheck verifies that the transaction's structural invariants hold
// against the chain's context and that it does not produce more funds than it
// consumes.
func (i *Import) SanityCheck(ctx *snow.Context) error {
	switch {
	case i.NetworkID != ctx.NetworkID:
		return fmt.Errorf("%w: want %d, got %d", errWrongNetworkID, ctx.NetworkID, i.NetworkID)
	case i.BlockchainID != ctx.ChainID:
		return fmt.Errorf("%w: want %s, got %s", errWrongChainID, ctx.ChainID, i.BlockchainID)
	case i.SourceChain != constants.PlatformChainID && i.SourceChain != ctx.XChainID:
		return fmt.Errorf("%w: want %s or %s, got %s", errNotSameSubnet, constants.PlatformChainID, ctx.XChainID, i.SourceChain)
	case len(i.ImportedInputs) == 0:
		return errNoInputs
	case len(i.Outs) == 0:
		return errNoOutputs
	}

	fc := avax.NewFlowChecker()
	for j, in := range i.ImportedInputs {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %w", errInvalidInput, j, err)
		}
		if assetID := in.Asset.ID; assetID != ctx.AVAXAssetID {
			return fmt.Errorf("%w (%d): want %s, got %s", errNonAVAXInput, j, ctx.AVAXAssetID, assetID)
		}
		fc.Consume(ctx.AVAXAssetID, in.In.Amount())
	}
	for j, out := range i.Outs {
		if out.Amount == 0 {
			return fmt.Errorf("%w (%d): zero amount", errInvalidOutput, j)
		}
		if out.AssetID != ctx.AVAXAssetID {
			return fmt.Errorf("%w (%d): want %s, got %s", errNonAVAXOutput, j, ctx.AVAXAssetID, out.AssetID)
		}
		fc.Produce(ctx.AVAXAssetID, out.Amount)
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
	errFetchingUTXOs           = errors.New("fetching UTXOs")
	errConvertingToFxTx        = errors.New("converting to fx transaction")
	errUnmarshallingUTXO       = errors.New("unmarshalling UTXO")
	errMismatchedAssetIDs      = errors.New("mismatched asset IDs")
	errVerifyingTransfer       = errors.New("verifying transfer")
)

func (i *Import) verifyCredentials(sm chainsatomic.SharedMemory, creds []Credential) error {
	if len(i.ImportedInputs) != len(creds) {
		return fmt.Errorf("%w: expected %d, got %d", errIncorrectNumCredentials, len(i.ImportedInputs), len(creds))
	}

	fxTx, err := toFxTx(i)
	if err != nil {
		return fmt.Errorf("%w: %w", errConvertingToFxTx, err)
	}

	utxoIDs := make([][]byte, len(i.ImportedInputs))
	for i, in := range i.ImportedInputs {
		inputID := in.UTXOID.InputID()
		utxoIDs[i] = inputID[:]
	}

	utxoBytes, err := sm.Get(i.SourceChain, utxoIDs)
	if err != nil {
		return fmt.Errorf("%w from %s: %w", errFetchingUTXOs, i.SourceChain, err)
	}

	for i, in := range i.ImportedInputs {
		// TODO(StephenButtolph): Parallelize transfer verification, which
		// includes signature verification. This is non-trivial, because
		// transactions frequently contain duplicate signatures, which are
		// currently being cached.
		utxo := &avax.UTXO{}
		if _, err := c.Unmarshal(utxoBytes[i], utxo); err != nil {
			return fmt.Errorf("%w: %w", errUnmarshallingUTXO, err)
		}
		if inAssetID, utxoAssetID := in.AssetID(), utxo.AssetID(); utxoAssetID != inAssetID {
			return fmt.Errorf("%w: input asset ID %s does not match UTXO asset ID %s", errMismatchedAssetIDs, inAssetID, utxoAssetID)
		}
		if err := fx.VerifyTransfer(fxTx, in.In, creds[i], utxo.Out); err != nil {
			return fmt.Errorf("%w: %w", errVerifyingTransfer, err)
		}
	}
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

func (i *Import) asOp(avaxAssetID ids.ID) (op, error) {
	mint := make(map[common.Address]uint256.Int, len(i.Outs))
	for _, out := range i.Outs {
		if out.AssetID != avaxAssetID {
			continue
		}

		var (
			total  = mint[out.Address]
			amount = scaleAVAX(out.Amount)
		)
		if _, overflow := total.AddOverflow(&total, &amount); overflow {
			return op{}, fmt.Errorf("%w: for address %s", errOverflow, out.Address)
		}
		mint[out.Address] = total
	}
	return op{
		mint: mint,
	}, nil
}

func (i *Import) atomicRequests(ids.ID) (ids.ID, *chainsatomic.Requests, error) {
	utxoIDs := make([][]byte, len(i.ImportedInputs))
	for j, in := range i.ImportedInputs {
		inputID := in.InputID()
		utxoIDs[j] = inputID[:]
	}
	return i.SourceChain, &chainsatomic.Requests{RemoveRequests: utxoIDs}, nil
}

// TransferNonAVAX adds the non-AVAX balances to the statedb.
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
