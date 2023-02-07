// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Max number of items allowed in a page
const MaxPageSize = 1024

var (
	// 0x010000000000000000000000000000000000000c
	// P-kopernikus1qyqqqqqqqqqqqqqqqqqqqqqqqqqqqqqvy7p25h
	feeRewardAddr = ids.ShortID{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x0c,
	}
	feeRewardAddrTraits = set.Set[ids.ShortID]{feeRewardAddr: struct{}{}}

	errWrongOutType = errors.New("wrong output type")
)

type caminoStateChanges struct {
	AtomicInputs                  set.Set[ids.ID]
	AtomicRequests                map[ids.ID]*atomic.Requests
	AddedUTXOs                    []*avax.UTXO
	Claimables                    map[ids.ID]*state.Claimable
	LastRewardImportTimestamp     *uint64
	NotDistributedValidatorReward *uint64
}

func (cs *caminoStateChanges) Apply(stateDiff state.Diff) {
	for _, utxo := range cs.AddedUTXOs {
		stateDiff.AddUTXO(utxo)
	}
	for ownerID, claimable := range cs.Claimables {
		stateDiff.SetClaimable(ownerID, claimable)
	}
	if cs.LastRewardImportTimestamp != nil {
		stateDiff.SetLastRewardImportTimestamp(*cs.LastRewardImportTimestamp)
	}
	if cs.NotDistributedValidatorReward != nil {
		stateDiff.SetNotDistributedValidatorReward(*cs.NotDistributedValidatorReward)
	}
}

func (cs *caminoStateChanges) AtomicChanges() (
	inputs set.Set[ids.ID],
	requests map[ids.ID]*atomic.Requests,
) {
	return cs.AtomicInputs, cs.AtomicRequests
}

func (cs *caminoStateChanges) Len() int {
	base := 0
	if cs.LastRewardImportTimestamp != nil {
		base++
	}
	if cs.NotDistributedValidatorReward != nil {
		base++
	}
	return base + cs.AtomicInputs.Len() + len(cs.AtomicRequests) + len(cs.AddedUTXOs)
}

func caminoAdvanceTimeTo(
	backend *Backend,
	parentState state.Chain,
	newChainTime time.Time,
	changes *stateChanges,
) error {
	if backend.Config.CaminoConfig.ValidatorsRewardPeriod == 0 {
		return nil
	}

	nextValidatorsRewardTime := getNextValidatorsRewardTime(
		uint64(parentState.GetTimestamp().Unix()),
		backend.Config.CaminoConfig.ValidatorsRewardPeriod,
	)

	if !nextValidatorsRewardTime.After(newChainTime) {
		// Getting imported reward utxos

		atomicUTXOs, _, _, err := backend.AtomicUTXOManager.GetAtomicUTXOs(
			backend.Ctx.CChainID,
			feeRewardAddrTraits,
			ids.ShortEmpty, ids.Empty, MaxPageSize,
		)
		if err != nil {
			return fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
		}

		lastRewardImportTimestamp := uint64(newChainTime.Unix())
		changes.LastRewardImportTimestamp = &lastRewardImportTimestamp

		if len(atomicUTXOs) == 0 {
			return nil
		}

		// Calculating imported amount, getting utxoids

		changes.AtomicInputs = set.NewSet[ids.ID](len(atomicUTXOs))
		utxoIDs := make([][]byte, len(atomicUTXOs))
		importedAmount := uint64(0)
		for i, utxo := range atomicUTXOs {
			utxoID := utxo.InputID()
			utxoIDs[i] = utxoID[:]
			changes.AtomicInputs.Add(utxoID)
			secpOut, ok := utxo.Out.(*secp256k1fx.TransferOutput)
			if !ok {
				return errWrongOutType
			}
			importedAmount, err = math.Add64(importedAmount, secpOut.Amt)
			if err != nil {
				return fmt.Errorf("can't compact imported UTXOs: %w", err)
			}
		}
		unsignedBytes, err := txs.Codec.Marshal(txs.Version, utxoIDs)
		if err != nil {
			return fmt.Errorf("failed to marhsal atomic UTXOs ids: %w", err)
		}

		txID, err := ids.ToID(hashing.ComputeHash256(unsignedBytes))
		if err != nil {
			return fmt.Errorf("failed to generate id out of atomic UTXOs ids: %w", err)
		}

		// Calculating and setting validators rewards

		currentStakerIterator, err := parentState.GetCurrentStakerIterator()
		if err != nil {
			return err
		}
		defer currentStakerIterator.Release()

		validators := set.Set[ids.ShortID]{}
		for currentStakerIterator.Next() {
			staker := currentStakerIterator.Value()
			if staker.SubnetID != constants.PrimaryNetworkID {
				continue
			}

			validatorAddr, err := parentState.GetShortIDLink(
				ids.ShortID(staker.NodeID), state.ShortLinkKeyRegisterNode,
			)
			if err != nil {
				return err
			}
			validators.Add(validatorAddr)
		}

		notDistributedAmount, err := parentState.GetNotDistributedValidatorReward()
		if err != nil {
			return err
		}

		amountToDistribute, err := math.Add64(importedAmount, notDistributedAmount)
		if err != nil {
			return err
		}

		addedReward := amountToDistribute / uint64(validators.Len())
		newNotDistributedAmount := amountToDistribute - addedReward*uint64(validators.Len())

		if newNotDistributedAmount != notDistributedAmount {
			changes.NotDistributedValidatorReward = &newNotDistributedAmount
		}

		if addedReward != 0 {
			changes.Claimables = make(map[ids.ID]*state.Claimable, validators.Len())
			for validatorAddr := range validators {
				owner := &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{validatorAddr},
				}

				ownerID, err := txs.GetOwnerID(owner)
				if err != nil {
					return err
				}

				claimable, err := parentState.GetClaimable(ownerID)
				if err != nil {
					return err
				}

				if claimable == nil {
					claimable = &state.Claimable{
						Owner: owner,
					}
				}

				newValidatorReward, err := math.Add64(claimable.ValidatorReward, addedReward)
				if err != nil {
					return err
				}
				claimable.ValidatorReward = newValidatorReward
				changes.Claimables[ownerID] = claimable
			}
		}

		// Producing reward utxo

		changes.AddedUTXOs = append(changes.AddedUTXOs, &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: backend.Ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: importedAmount,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{feeRewardAddr},
				},
			},
		})

		changes.AtomicRequests = map[ids.ID]*atomic.Requests{
			backend.Ctx.CChainID: {
				RemoveRequests: utxoIDs,
			},
		}
	}

	return nil
}
