// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// SpendResult holds the outputs of a Spend operation.
type SpendResult struct {
	Inputs        []*avax.TransferableInput
	ChangeOutputs []*avax.TransferableOutput
	StakeOutputs  []*avax.TransferableOutput
}

// Spend selects UTXOs from the provided backend to satisfy the given burn and
// stake requirements, accounting for fees based on the provided base
// complexity.
//
//   - [toBurn] maps assetID to the amount of the asset to spend without
//     producing an output. This is typically used for fees. However, it can
//     also be used to consume some of an asset that will be produced in
//     separate outputs, such as ExportedOutputs. Only unlocked UTXOs are able
//     to be burned here.
//   - [toStake] maps assetID to the amount of the asset to spend and place into
//     the staked outputs. First locked UTXOs are attempted to be used for these
//     funds, and then unlocked UTXOs will be attempted to be used. There is no
//     preferential ordering on the unlock times.
//   - [excessAVAX] contains the amount of extra AVAX that Spend can produce in
//     the change outputs in addition to the consumed and not burned AVAX.
//   - [complexity] contains the currently accrued transaction complexity that
//     will be used to calculate the required fees to be burned.
//   - [ownerOverride] optionally specifies the output owners to use for the
//     unlocked AVAX change output if no additional AVAX was needed to be
//     burned. If this value is nil, the default change owner is used.
func Spend(
	backend Backend,
	context *Context,
	addrs set.Set[ids.ShortID],
	toBurn map[ids.ID]uint64,
	toStake map[ids.ID]uint64,
	excessAVAX uint64,
	complexity gas.Dimensions,
	ownerOverride *secp256k1fx.OutputOwners,
	options *common.Options,
) (*SpendResult, error) {
	utxos, err := backend.UTXOs(options.Context(), constants.PlatformChainID)
	if err != nil {
		return nil, err
	}

	addrs = options.Addresses(addrs)
	minIssuanceTime := options.MinIssuanceTime()

	addr, ok := addrs.Peek()
	if !ok {
		return nil, ErrNoChangeAddress
	}
	changeOwner := options.ChangeOwner(&secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	})
	if ownerOverride == nil {
		ownerOverride = changeOwner
	}

	s := spendHelper{
		weights:  context.ComplexityWeights,
		gasPrice: context.GasPrice,

		toBurn:     toBurn,
		toStake:    toStake,
		complexity: complexity,

		// Initialize the return values with empty slices to preserve backward
		// compatibility of the json representation of transactions with no
		// inputs or outputs.
		inputs:        []*avax.TransferableInput{},
		changeOutputs: []*avax.TransferableOutput{},
		stakeOutputs:  []*avax.TransferableOutput{},
	}

	utxosByLocktime := splitByLocktime(utxos, minIssuanceTime)
	for _, utxo := range utxosByLocktime.locked {
		assetID := utxo.AssetID()
		if !s.shouldConsumeLockedAsset(assetID) {
			continue
		}

		out, locktime, err := unwrapOutput(utxo.Out)
		if err != nil {
			return nil, err
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		err = s.addInput(&avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &stakeable.LockIn{
				Locktime: locktime,
				TransferableIn: &secp256k1fx.TransferInput{
					Amt: out.Amt,
					Input: secp256k1fx.Input{
						SigIndices: inputSigIndices,
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}

		excess := s.consumeLockedAsset(assetID, out.Amt)
		err = s.addStakedOutput(&avax.TransferableOutput{
			Asset: utxo.Asset,
			Out: &stakeable.LockOut{
				Locktime: locktime,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt:          out.Amt - excess,
					OutputOwners: out.OutputOwners,
				},
			},
		})
		if err != nil {
			return nil, err
		}

		if excess == 0 {
			continue
		}

		// This input had extra value, so some of it must be returned
		err = s.addChangeOutput(&avax.TransferableOutput{
			Asset: utxo.Asset,
			Out: &stakeable.LockOut{
				Locktime: locktime,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt:          excess,
					OutputOwners: out.OutputOwners,
				},
			},
		})
		if err != nil {
			return nil, err
		}
	}

	// Add all the remaining stake amounts assuming unlocked UTXOs.
	for assetID, amount := range s.toStake {
		if amount == 0 {
			continue
		}

		err = s.addStakedOutput(&avax.TransferableOutput{
			Asset: avax.Asset{
				ID: assetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: *changeOwner,
			},
		})
		if err != nil {
			return nil, err
		}
	}

	// AVAX is handled last to account for fees.
	utxosByAVAXAssetID := splitByAssetID(utxosByLocktime.unlocked, context.AVAXAssetID)
	for _, utxo := range utxosByAVAXAssetID.other {
		assetID := utxo.AssetID()
		if !s.shouldConsumeAsset(assetID) {
			continue
		}

		out, _, err := unwrapOutput(utxo.Out)
		if err != nil {
			return nil, err
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		err = s.addInput(&avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt: out.Amt,
				Input: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
			},
		})
		if err != nil {
			return nil, err
		}

		excess := s.consumeAsset(assetID, out.Amt)
		if excess == 0 {
			continue
		}

		// This input had extra value, so some of it must be returned
		err = s.addChangeOutput(&avax.TransferableOutput{
			Asset: utxo.Asset,
			Out: &secp256k1fx.TransferOutput{
				Amt:          excess,
				OutputOwners: *changeOwner,
			},
		})
		if err != nil {
			return nil, err
		}
	}

	for _, utxo := range utxosByAVAXAssetID.requested {
		requiredFee, err := s.calculateFee()
		if err != nil {
			return nil, err
		}

		// If we don't need to burn or stake additional AVAX and we have
		// consumed enough AVAX to pay the required fee, we should stop
		// consuming UTXOs.
		if !s.shouldConsumeAsset(context.AVAXAssetID) && excessAVAX >= requiredFee {
			break
		}

		out, _, err := unwrapOutput(utxo.Out)
		if err != nil {
			return nil, err
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		err = s.addInput(&avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt: out.Amt,
				Input: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
			},
		})
		if err != nil {
			return nil, err
		}

		excess := s.consumeAsset(context.AVAXAssetID, out.Amt)
		excessAVAX, err = math.Add(excessAVAX, excess)
		if err != nil {
			return nil, err
		}

		// If we need to consume additional AVAX, we should be returning the
		// change to the change address.
		ownerOverride = changeOwner
	}

	if err := s.verifyAssetsConsumed(); err != nil {
		return nil, err
	}

	requiredFee, err := s.calculateFee()
	if err != nil {
		return nil, err
	}
	if excessAVAX < requiredFee {
		return nil, fmt.Errorf(
			"%w: provided UTXOs needed %d more nAVAX (%q)",
			ErrInsufficientFunds,
			requiredFee-excessAVAX,
			context.AVAXAssetID,
		)
	}

	secpExcessAVAXOutput := &secp256k1fx.TransferOutput{
		Amt:          0, // Populated later if used
		OutputOwners: *ownerOverride,
	}
	excessAVAXOutput := &avax.TransferableOutput{
		Asset: avax.Asset{
			ID: context.AVAXAssetID,
		},
		Out: secpExcessAVAXOutput,
	}
	if err := s.addOutputComplexity(excessAVAXOutput); err != nil {
		return nil, err
	}

	requiredFeeWithChange, err := s.calculateFee()
	if err != nil {
		return nil, err
	}
	if excessAVAX > requiredFeeWithChange {
		// It is worth adding the change output
		secpExcessAVAXOutput.Amt = excessAVAX - requiredFeeWithChange
		s.changeOutputs = append(s.changeOutputs, excessAVAXOutput)
	}

	utils.Sort(s.inputs)                                     // sort inputs
	avax.SortTransferableOutputs(s.changeOutputs, txs.Codec) // sort the change outputs
	avax.SortTransferableOutputs(s.stakeOutputs, txs.Codec)  // sort stake outputs
	return &SpendResult{
		Inputs:        s.inputs,
		ChangeOutputs: s.changeOutputs,
		StakeOutputs:  s.stakeOutputs,
	}, nil
}
