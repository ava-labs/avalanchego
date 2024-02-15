// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/avm/txs/fees"
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
	// FinanceTx the provided amount while deducting the provided fee.
	// Arguments:
	// - [utxos] contains assets ID and amount to be spend for each assestID
	// - [kc] are the owners of the funds
	// - [toSpend] is the amount of funds that are available to be spent for each assetID
	// Returns:
	// - [inputs] the inputs that should be consumed to fund the outputs
	// - [outputs] the outputs produced
	// - [signers] the proof of ownership of the funds being moved
	FinanceTx(
		utxos []*avax.UTXO,
		feeAssetID ids.ID,
		kc *secp256k1fx.Keychain,
		toSpend map[ids.ID]uint64,
		feeCalc *fees.Calculator,
		changeAddr ids.ShortID,
	) (
		[]*avax.TransferableInput, // inputs
		[]*avax.TransferableOutput, // outputs
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

func (s *spender) FinanceTx(
	utxos []*avax.UTXO,
	feeAssetID ids.ID,
	kc *secp256k1fx.Keychain,
	toSpend map[ids.ID]uint64,
	feeCalc *fees.Calculator,
	changeAddr ids.ShortID,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // outputs
	[][]*secp256k1.PrivateKey, // signers
	error,
) {
	// account for the fees accumulated so far
	amountWithFee, err := math.Add64(toSpend[feeAssetID], feeCalc.Fee)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("problem calculating required spend amount: %w", err)
	}
	toSpend[feeAssetID] = amountWithFee

	var (
		minIssuanceTime = s.clock.Unix()

		ins  = []*avax.TransferableInput{}
		outs = []*avax.TransferableOutput{}
		keys = [][]*secp256k1.PrivateKey{}
	)

	// Iterate over the UTXOs
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		remainingAmountToBurn := toSpend[assetID]

		// If we have consumed enough of the asset, then we have no need burn
		// more.
		if remainingAmountToBurn == 0 {
			continue
		}

		inputIntf, signers, err := kc.Spend(utxo.Out, minIssuanceTime)
		if err != nil {
			// this utxo can't be spent with the current keys right now
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			// this input doesn't have an amount, so I don't care about it here
			continue
		}

		amountToBurn := min(input.Amount(), toSpend[assetID])
		toSpend[assetID] -= amountToBurn // no overflow here

		in := &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: assetID},
			In:     input,
		}

		// update fees to target given the extra input added
		inputFees, err := fees.FinanceInput(feeCalc, s.codec, in)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		toSpendWithFees, err := math.Add64(toSpend[feeAssetID], inputFees)
		if err != nil {
			return nil, nil, nil, errSpendOverflow
		}
		toSpend[feeAssetID] = toSpendWithFees

		// account for credentials as well
		credFees, err := fees.FinanceCredential(feeCalc, s.codec, len(signers))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for credential fees: %w", err)
		}
		toSpendWithFees, err = math.Add64(toSpend[feeAssetID], credFees)
		if err != nil {
			return nil, nil, nil, errSpendOverflow
		}
		toSpend[feeAssetID] = toSpendWithFees

		ins = append(ins, in)
		keys = append(keys, signers)

		if assetID != feeAssetID {
			// can't pay fees in non-avax tokens. Move on
			continue
		}

		// add change if needed
		if remainingAmt := input.Amount() - amountToBurn; inputFees+credFees < remainingAmt {
			// input has not been fully spent, so we may have to add a change.
			// Note that change must pay for its own fees
			remainingAmt -= inputFees + credFees
			out := &avax.TransferableOutput{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: remainingAmt,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{changeAddr},
					},
				},
			}

			outputFees, addedUnits, err := fees.FinanceOutput(feeCalc, s.codec, out)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("account for credential fees: %w", err)
			}

			switch {
			case remainingAmt > outputFees:
				// utxo is large enough to pay its own fees
				out.Out.(*secp256k1fx.TransferOutput).Amt = remainingAmt - outputFees
				outs = append(outs, out)
				toSpend[assetID] = 0
			case remainingAmt == outputFees:
				// utxo matches the amount to burn and the fees of the output.
				// However output would have a zero amount. We fully burn the input
				// without adding a zero amount output
				toSpend[assetID] = 0

			case remainingAmt < outputFees:
				// utxo is not large enough to pay its own fees.
				// we don't add the utxo, fully burn the input and move
				// to the next utxos to complete tx financing
				if _, err := feeCalc.RemoveFeesFor(addedUnits); err != nil {
					return nil, nil, nil, fmt.Errorf("failed removing output: %w", err)
				}
				toSpend[assetID] -= remainingAmt
			}
		}
	}

	for asset, amount := range toSpend {
		if toSpend[asset] != 0 {
			return nil, nil, nil, fmt.Errorf("want to spend %d of asset %s but only have %d",
				amount,
				asset,
				toSpend[asset],
			)
		}
	}

	avax.SortTransferableInputsWithSigners(ins, keys)
	avax.SortTransferableOutputs(outs, s.codec)
	return ins, outs, keys, nil
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
		// makes sure that the variable isn't overwritten with the next iteration
		utxo := utxo

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
		newAmountSpent, err := math.Add64(amountSpent, input.Amount())
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
		// makes sure that the variable isn't overwritten with the next iteration
		utxo := utxo

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
		// makes sure that the variable isn't overwritten with the next iteration
		utxo := utxo

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
