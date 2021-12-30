// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var (
	errShouldBeDSValidator = errors.New("expected validator to be in the primary network")
	errWrongTxType         = errors.New("wrong transaction type")

	_ UnsignedProposalTx = &UnsignedRewardValidatorTx{}
)

// UnsignedRewardValidatorTx is a transaction that represents a proposal to
// remove a validator that is currently validating from the validator set.
//
// If this transaction is accepted and the next block accepted is a Commit
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX as well as a validating reward.
//
// If this transaction is accepted and the next block accepted is an Abort
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX but no reward.
type UnsignedRewardValidatorTx struct {
	avax.Metadata

	// ID of the tx that created the delegator/validator being removed/rewarded
	TxID ids.ID `serialize:"true" json:"txID"`

	// Marks if this validator should be rewarded according to this node.
	shouldPreferCommit bool
}

func (tx *UnsignedRewardValidatorTx) InitCtx(*snow.Context) {}

func (tx *UnsignedRewardValidatorTx) InputIDs() ids.Set {
	return nil
}

func (tx *UnsignedRewardValidatorTx) SyntacticVerify(*snow.Context) error {
	return nil
}

// Attempts to verify this transaction with the provided state.
func (tx *UnsignedRewardValidatorTx) SemanticVerify(vm *VM, parentState MutableState, stx *Tx) error {
	_, _, err := tx.Execute(vm, parentState, stx)
	return err
}

// Execute this transaction.
//
// The current validating set must have at least one member.
// The next validator to be removed must be the validator specified in this block.
// The next validator to be removed must be have an end time equal to the current
//   chain timestamp.
func (tx *UnsignedRewardValidatorTx) Execute(
	vm *VM,
	parentState MutableState,
	stx *Tx,
) (
	VersionedState,
	VersionedState,
	error,
) {
	switch {
	case tx == nil:
		return nil, nil, errNilTx
	case tx.TxID == ids.Empty:
		return nil, nil, errInvalidID
	case len(stx.Creds) != 0:
		return nil, nil, errWrongNumberOfCredentials
	}

	currentStakers := parentState.CurrentStakerChainState()
	stakerTx, stakerReward, err := currentStakers.GetNextStaker()
	if err == database.ErrNotFound {
		return nil, nil, fmt.Errorf("failed to get next staker stop time: %w", err)
	}
	if err != nil {
		return nil, nil, err
	}

	stakerID := stakerTx.ID()
	if stakerID != tx.TxID {
		return nil, nil, fmt.Errorf(
			"attempting to remove TxID: %s. Should be removing %s",
			tx.TxID,
			stakerID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentTime := parentState.GetTimestamp()
	staker, ok := stakerTx.UnsignedTx.(TimedTx)
	if !ok {
		return nil, nil, errWrongTxType
	}
	if endTime := staker.EndTime(); !endTime.Equal(currentTime) {
		return nil, nil, fmt.Errorf(
			"attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			endTime,
		)
	}

	newlyCurrentStakers, err := currentStakers.DeleteNextStaker()
	if err != nil {
		return nil, nil, err
	}

	pendingStakers := parentState.PendingStakerChainState()
	onCommitState := newVersionedState(parentState, newlyCurrentStakers, pendingStakers)
	onAbortState := newVersionedState(parentState, newlyCurrentStakers, pendingStakers)

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply := onAbortState.GetCurrentSupply()
	newSupply, err := math.Sub64(currentSupply, stakerReward)
	if err != nil {
		return nil, nil, err
	}
	onAbortState.SetCurrentSupply(newSupply)

	var (
		nodeID    ids.ShortID
		startTime time.Time
	)
	switch uStakerTx := stakerTx.UnsignedTx.(type) {
	case *UnsignedAddValidatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			onCommitState.AddUTXO(utxo)
			onAbortState.AddUTXO(utxo)
		}

		// Provide the reward here
		if stakerReward > 0 {
			outIntf, err := vm.fx.CreateOutput(stakerReward, uStakerTx.RewardsOwner)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, errInvalidState
			}

			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out:   out,
			}

			onCommitState.AddUTXO(utxo)
			onCommitState.AddRewardUTXO(tx.TxID, utxo)
		}

		// Handle reward preferences
		nodeID = uStakerTx.Validator.ID()
		startTime = uStakerTx.StartTime()
	case *UnsignedAddDelegatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			onCommitState.AddUTXO(utxo)
			onAbortState.AddUTXO(utxo)
		}

		// We're removing a delegator, so we need to fetch the validator they
		// are delgated to.
		vdr, err := currentStakers.GetValidator(uStakerTx.Validator.NodeID)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				uStakerTx.Validator.NodeID,
				err,
			)
		}
		vdrTx := vdr.AddValidatorTx()

		// Calculate split of reward between delegator/delegatee
		// The delegator gives stake to the validatee
		delegatorShares := reward.PercentDenominator - uint64(vdrTx.Shares)             // parentTx.Shares <= reward.PercentDenominator so no underflow
		delegatorReward := delegatorShares * (stakerReward / reward.PercentDenominator) // delegatorShares <= reward.PercentDenominator so no overflow
		// Delay rounding as long as possible for small numbers
		if optimisticReward, err := math.Mul64(delegatorShares, stakerReward); err == nil {
			delegatorReward = optimisticReward / reward.PercentDenominator
		}
		delegateeReward := stakerReward - delegatorReward // delegatorReward <= reward so no underflow

		offset := 0

		// Reward the delegator here
		if delegatorReward > 0 {
			outIntf, err := vm.fx.CreateOutput(delegatorReward, uStakerTx.RewardsOwner)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, errInvalidState
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out:   out,
			}

			onCommitState.AddUTXO(utxo)
			onCommitState.AddRewardUTXO(tx.TxID, utxo)

			offset++
		}

		// Reward the delegatee here
		if delegateeReward > 0 {
			outIntf, err := vm.fx.CreateOutput(delegateeReward, vdrTx.RewardsOwner)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, errInvalidState
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake) + offset),
				},
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out:   out,
			}

			onCommitState.AddUTXO(utxo)
			onCommitState.AddRewardUTXO(tx.TxID, utxo)
		}

		nodeID = uStakerTx.Validator.ID()
		startTime = vdrTx.StartTime()
	default:
		return nil, nil, errShouldBeDSValidator
	}

	uptime, err := vm.uptimeManager.CalculateUptimePercentFrom(nodeID, startTime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to calculate uptime: %w", err)
	}
	tx.shouldPreferCommit = uptime >= vm.UptimePercentage

	return onCommitState, onAbortState, nil
}

// InitiallyPrefersCommit returns true if this node thinks the validator
// should receive a staking reward.
//
// TODO: A validator should receive a reward only if they are sufficiently
// responsive and correct during the time they are validating.
// Right now they receive a reward if they're up (but not necessarily
// correct and responsive) for a sufficient amount of time
func (tx *UnsignedRewardValidatorTx) InitiallyPrefersCommit(*VM) bool {
	return tx.shouldPreferCommit
}

// RewardStakerTx creates a new transaction that proposes to remove the staker
// [validatorID] from the default validator set.
func (vm *VM) newRewardValidatorTx(txID ids.ID) (*Tx, error) {
	tx := &Tx{UnsignedTx: &UnsignedRewardValidatorTx{
		TxID: txID,
	}}
	return tx, tx.Sign(Codec, nil)
}
