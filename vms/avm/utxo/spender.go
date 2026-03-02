// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errSpendOverflow          = errors.New("spent amount overflows uint64")
	errInsufficientFunds      = errors.New("insufficient funds")
	errAddressesCantMintAsset = errors.New("provided addresses don't have the authority to mint the provided asset")
)

type Spender interface {
	// Spend the provided amount while deducting the provided fee.
	// Arguments:
	// - [utxos] contains assets ID and amount to be spend for each assestID
	// - [kc] are the owners of the funds
	// - [amounts] is the amount of funds that are available to be spent for each assetID
	// Returns:
	// - [amountsSpent] the amount of funds that are spent
	// - [inputs] the inputs that should be consumed to fund the outputs
	// - [signers] the proof of ownership of the funds being moved
	Spend(
		utxos []*avax.UTXO,
		kc *secp256k1fx.Keychain,
		amounts map[ids.ID]uint64,
	) (
		map[ids.ID]uint64, // amountsSpent
		[]*avax.TransferableInput, // inputs
		[][]*secp256k1.PrivateKey, // signers
		error,
	)

	SpendNFT(
		utxos []*avax.UTXO,
		kc *secp256k1fx.Keychain,
		assetID ids.ID,
		groupID uint32,
		to ids.ShortID,
	) (
		[]*txs.Operation,
		[][]*secp256k1.PrivateKey,
		error,
	)

	SpendAll(
		utxos []*avax.UTXO,
		kc *secp256k1fx.Keychain,
	) (
		map[ids.ID]uint64,
		[]*avax.TransferableInput,
		[][]*secp256k1.PrivateKey,
		error,
	)

	Mint(
		utxos []*avax.UTXO,
		kc *secp256k1fx.Keychain,
		amounts map[ids.ID]uint64,
		to ids.ShortID,
	) (
		[]*txs.Operation,
		[][]*secp256k1.PrivateKey,
		error,
	)

	MintNFT(
		utxos []*avax.UTXO,
		kc *secp256k1fx.Keychain,
		assetID ids.ID,
		payload []byte,
		to ids.ShortID,
	) (
		[]*txs.Operation,
		[][]*secp256k1.PrivateKey,
		error,
	)
}

func NewSpender(
	clk *mockable.Clock,
	codec codec.Manager,
) Spender {
	return &spender{
		clock: clk,
		codec: codec,
	}
}

type spender struct {
	clock *mockable.Clock
	codec codec.Manager
}

func (s *spender) Spend(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	amounts map[ids.ID]uint64,
) (
	map[ids.ID]uint64, // amountsSpent
	[]*avax.TransferableInput, // inputs
	[][]*secp256k1.PrivateKey, // signers
	error,
) {
	amountsSpent := make(map[ids.ID]uint64, len(amounts))
	time := s.clock.Unix()

	ins := []*avax.TransferableInput{}
	keys := [][]*secp256k1.PrivateKey{}
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		amount := amounts[assetID]
		amountSpent := amountsSpent[assetID]

		if amountSpent >= amount {
			// we already have enough inputs allocated to this asset
			continue
		}

		inputIntf, signers, err := kc.Spend(utxo.Out, time)
		if err != nil {
			// this utxo can't be spent with the current keys right now
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			// this input doesn't have an amount, so I don't care about it here
			continue
		}
		newAmountSpent, err := math.Add(amountSpent, input.Amount())
		if err != nil {
			// there was an error calculating the consumed amount, just error
			return nil, nil, nil, errSpendOverflow
		}
		amountsSpent[assetID] = newAmountSpent

		// add the new input to the array
		ins = append(ins, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: assetID},
			In:     input,
		})
		// add the required keys to the array
		keys = append(keys, signers)
	}

	for asset, amount := range amounts {
		if amountsSpent[asset] < amount {
			return nil, nil, nil, fmt.Errorf("want to spend %d of asset %s but only have %d",
				amount,
				asset,
				amountsSpent[asset],
			)
		}
	}

	avax.SortTransferableInputsWithSigners(ins, keys)
	return amountsSpent, ins, keys, nil
}

func (s *spender) SpendNFT(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	assetID ids.ID,
	groupID uint32,
	to ids.ShortID,
) (
	[]*txs.Operation,
	[][]*secp256k1.PrivateKey,
	error,
) {
	time := s.clock.Unix()

	ops := []*txs.Operation{}
	keys := [][]*secp256k1.PrivateKey{}

	for _, utxo := range utxos {
		if len(ops) > 0 {
			// we have already been able to create the operation needed
			break
		}

		if utxo.AssetID() != assetID {
			// wrong asset ID
			continue
		}
		out, ok := utxo.Out.(*nftfx.TransferOutput)
		if !ok {
			// wrong output type
			continue
		}
		if out.GroupID != groupID {
			// wrong group id
			continue
		}
		indices, signers, ok := kc.Match(&out.OutputOwners, time)
		if !ok {
			// unable to spend the output
			continue
		}

		// add the new operation to the array
		ops = append(ops, &txs.Operation{
			Asset:   utxo.Asset,
			UTXOIDs: []*avax.UTXOID{&utxo.UTXOID},
			Op: &nftfx.TransferOperation{
				Input: secp256k1fx.Input{
					SigIndices: indices,
				},
				Output: nftfx.TransferOutput{
					GroupID: out.GroupID,
					Payload: out.Payload,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{to},
					},
				},
			},
		})
		// add the required keys to the array
		keys = append(keys, signers)
	}

	if len(ops) == 0 {
		return nil, nil, errInsufficientFunds
	}

	txs.SortOperationsWithSigners(ops, keys, s.codec)
	return ops, keys, nil
}

func (s *spender) SpendAll(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
) (
	map[ids.ID]uint64,
	[]*avax.TransferableInput,
	[][]*secp256k1.PrivateKey,
	error,
) {
	amountsSpent := make(map[ids.ID]uint64)
	time := s.clock.Unix()

	ins := []*avax.TransferableInput{}
	keys := [][]*secp256k1.PrivateKey{}
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		amountSpent := amountsSpent[assetID]

		inputIntf, signers, err := kc.Spend(utxo.Out, time)
		if err != nil {
			// this utxo can't be spent with the current keys right now
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			// this input doesn't have an amount, so I don't care about it here
			continue
		}
		newAmountSpent, err := math.Add(amountSpent, input.Amount())
		if err != nil {
			// there was an error calculating the consumed amount, just error
			return nil, nil, nil, errSpendOverflow
		}
		amountsSpent[assetID] = newAmountSpent

		// add the new input to the array
		ins = append(ins, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: assetID},
			In:     input,
		})
		// add the required keys to the array
		keys = append(keys, signers)
	}

	avax.SortTransferableInputsWithSigners(ins, keys)
	return amountsSpent, ins, keys, nil
}

func (s *spender) Mint(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	amounts map[ids.ID]uint64,
	to ids.ShortID,
) (
	[]*txs.Operation,
	[][]*secp256k1.PrivateKey,
	error,
) {
	time := s.clock.Unix()

	ops := []*txs.Operation{}
	keys := [][]*secp256k1.PrivateKey{}

	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		amount := amounts[assetID]
		if amount == 0 {
			continue
		}

		out, ok := utxo.Out.(*secp256k1fx.MintOutput)
		if !ok {
			continue
		}

		inIntf, signers, err := kc.Spend(out, time)
		if err != nil {
			continue
		}

		in, ok := inIntf.(*secp256k1fx.Input)
		if !ok {
			continue
		}

		// add the operation to the array
		ops = append(ops, &txs.Operation{
			Asset:   utxo.Asset,
			UTXOIDs: []*avax.UTXOID{&utxo.UTXOID},
			Op: &secp256k1fx.MintOperation{
				MintInput:  *in,
				MintOutput: *out,
				TransferOutput: secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{to},
					},
				},
			},
		})
		// add the required keys to the array
		keys = append(keys, signers)

		// remove the asset from the required amounts to mint
		delete(amounts, assetID)
	}

	for _, amount := range amounts {
		if amount > 0 {
			return nil, nil, errAddressesCantMintAsset
		}
	}

	txs.SortOperationsWithSigners(ops, keys, s.codec)
	return ops, keys, nil
}

func (s *spender) MintNFT(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	assetID ids.ID,
	payload []byte,
	to ids.ShortID,
) (
	[]*txs.Operation,
	[][]*secp256k1.PrivateKey,
	error,
) {
	time := s.clock.Unix()

	ops := []*txs.Operation{}
	keys := [][]*secp256k1.PrivateKey{}

	for _, utxo := range utxos {
		if len(ops) > 0 {
			// we have already been able to create the operation needed
			break
		}

		if utxo.AssetID() != assetID {
			// wrong asset id
			continue
		}
		out, ok := utxo.Out.(*nftfx.MintOutput)
		if !ok {
			// wrong output type
			continue
		}

		indices, signers, ok := kc.Match(&out.OutputOwners, time)
		if !ok {
			// unable to spend the output
			continue
		}

		// add the operation to the array
		ops = append(ops, &txs.Operation{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{
				&utxo.UTXOID,
			},
			Op: &nftfx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: indices,
				},
				GroupID: out.GroupID,
				Payload: payload,
				Outputs: []*secp256k1fx.OutputOwners{{
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				}},
			},
		})
		// add the required keys to the array
		keys = append(keys, signers)
	}

	if len(ops) == 0 {
		return nil, nil, errAddressesCantMintAsset
	}

	txs.SortOperationsWithSigners(ops, keys, s.codec)
	return ops, keys, nil
}
