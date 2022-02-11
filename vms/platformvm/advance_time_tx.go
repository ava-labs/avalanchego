// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var _ UnsignedProposalTx = &UnsignedAdvanceTimeTx{}

// UnsignedAdvanceTimeTx is a transaction to increase the chain's timestamp.
// When the chain's timestamp is updated (a AdvanceTimeTx is accepted and
// followed by a commit block) the staker set is also updated accordingly.
// It must be that:
//   * proposed timestamp > [current chain time]
//   * proposed timestamp <= [time for next staker set change]
type UnsignedAdvanceTimeTx struct {
	avax.Metadata

	// Unix time this block proposes increasing the timestamp to
	Time uint64 `serialize:"true" json:"time"`
}

func (tx *UnsignedAdvanceTimeTx) InitCtx(*snow.Context) {}

// Timestamp returns the time this block is proposing the chain should be set to
func (tx *UnsignedAdvanceTimeTx) Timestamp() time.Time {
	return time.Unix(int64(tx.Time), 0)
}

func (tx *UnsignedAdvanceTimeTx) InputIDs() ids.Set {
	return nil
}

func (tx *UnsignedAdvanceTimeTx) SyntacticVerify(*snow.Context) error {
	return nil
}

// Attempts to verify this transaction with the provided state.
func (tx *UnsignedAdvanceTimeTx) SemanticVerify(vm *VM, parentState MutableState, stx *Tx) error {
	_, _, err := tx.Execute(vm, parentState, stx)
	return err
}

// Execute this transaction.
func (tx *UnsignedAdvanceTimeTx) Execute(
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
	case len(stx.Creds) != 0:
		return nil, nil, errWrongNumberOfCredentials
	}

	txTimestamp := tx.Timestamp()
	localTimestamp := vm.clock.Time()
	localTimestampPlusSync := localTimestamp.Add(syncBound)
	if localTimestampPlusSync.Before(txTimestamp) {
		return nil, nil, fmt.Errorf(
			"proposed time (%s) is too far in the future relative to local time (%s)",
			txTimestamp,
			localTimestamp,
		)
	}

	if chainTimestamp := parentState.GetTimestamp(); !txTimestamp.After(chainTimestamp) {
		return nil, nil, fmt.Errorf(
			"proposed timestamp (%s), not after current timestamp (%s)",
			txTimestamp,
			chainTimestamp,
		)
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	nextStakerChangeTime, err := vm.nextStakerChangeTime(parentState)
	if err != nil {
		return nil, nil, err
	}

	if txTimestamp.After(nextStakerChangeTime) {
		return nil, nil, fmt.Errorf(
			"proposed timestamp (%s) later than next staker change time (%s)",
			txTimestamp,
			nextStakerChangeTime,
		)
	}

	currentSupply := parentState.GetCurrentSupply()

	pendingStakers := parentState.PendingStakerChainState()
	toAddValidatorsWithRewardToCurrent := []*validatorReward(nil)
	toAddDelegatorsWithRewardToCurrent := []*validatorReward(nil)
	toAddWithoutRewardToCurrent := []*Tx(nil)
	numToRemoveFromPending := 0

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp. [pendingStakers.Stakers()] is sorted in order
	// of increasing startTime
pendingStakerLoop:
	for _, tx := range pendingStakers.Stakers() {
		switch staker := tx.UnsignedTx.(type) {
		case *UnsignedAddDelegatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			r := vm.rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = safemath.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, err
			}

			toAddDelegatorsWithRewardToCurrent = append(toAddDelegatorsWithRewardToCurrent, &validatorReward{
				addStakerTx:     tx,
				potentialReward: r,
			})
			numToRemoveFromPending++
		case *UnsignedAddValidatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			r := vm.rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = safemath.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, err
			}

			toAddValidatorsWithRewardToCurrent = append(toAddValidatorsWithRewardToCurrent, &validatorReward{
				addStakerTx:     tx,
				potentialReward: r,
			})
			numToRemoveFromPending++
		case *UnsignedAddSubnetValidatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			// If this staker should already be removed, then we should just
			// never add them.
			if staker.EndTime().After(txTimestamp) {
				toAddWithoutRewardToCurrent = append(toAddWithoutRewardToCurrent, tx)
			}
			numToRemoveFromPending++
		default:
			return nil, nil, fmt.Errorf("expected validator but got %T", tx.UnsignedTx)
		}
	}
	newlyPendingStakers := pendingStakers.DeleteStakers(numToRemoveFromPending)

	currentStakers := parentState.CurrentStakerChainState()
	numToRemoveFromCurrent := 0

	// Remove from the staker set any subnet validators whose endTime is at or
	// before the new timestamp
currentStakerLoop:
	for _, tx := range currentStakers.Stakers() {
		switch staker := tx.UnsignedTx.(type) {
		case *UnsignedAddSubnetValidatorTx:
			if staker.EndTime().After(txTimestamp) {
				break currentStakerLoop
			}

			numToRemoveFromCurrent++
		case *UnsignedAddValidatorTx, *UnsignedAddDelegatorTx:
			// We shouldn't be removing any primary network validators here
			break currentStakerLoop
		default:
			return nil, nil, errWrongTxType
		}
	}
	newlyCurrentStakers, err := currentStakers.UpdateStakers(
		toAddValidatorsWithRewardToCurrent,
		toAddDelegatorsWithRewardToCurrent,
		toAddWithoutRewardToCurrent,
		numToRemoveFromCurrent,
	)
	if err != nil {
		return nil, nil, err
	}

	onCommitState := newVersionedState(parentState, newlyCurrentStakers, newlyPendingStakers)
	onCommitState.SetTimestamp(txTimestamp)
	onCommitState.SetCurrentSupply(currentSupply)

	// State doesn't change if this proposal is aborted
	onAbortState := newVersionedState(parentState, currentStakers, pendingStakers)

	return onCommitState, onAbortState, nil
}

// InitiallyPrefersCommit returns true if the proposed time is at
// or before the current time plus the synchrony bound
func (tx *UnsignedAdvanceTimeTx) InitiallyPrefersCommit(vm *VM) bool {
	txTimestamp := tx.Timestamp()
	localTimestamp := vm.clock.Time()
	localTimestampPlusSync := localTimestamp.Add(syncBound)
	return !txTimestamp.After(localTimestampPlusSync)
}

// newAdvanceTimeTx creates a new tx that, if it is accepted and followed by a
// Commit block, will set the chain's timestamp to [timestamp].
func (vm *VM) newAdvanceTimeTx(timestamp time.Time) (*Tx, error) {
	tx := &Tx{UnsignedTx: &UnsignedAdvanceTimeTx{
		Time: uint64(timestamp.Unix()),
	}}
	return tx, tx.Sign(Codec, nil)
}
