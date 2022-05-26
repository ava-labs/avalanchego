// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var _ StatefulProposalTx = &StatefulAdvanceTimeTx{}

// StatefulAdvanceTimeTx is a transaction to increase the chain's timestamp.
// When the chain's timestamp is updated (a AdvanceTimeTx is accepted and
// followed by a commit block) the staker set is also updated accordingly.
// It must be that:
//   * proposed timestamp > [current chain time]
//   * proposed timestamp <= [time for next staker set change]
type StatefulAdvanceTimeTx struct {
	*unsigned.AdvanceTimeTx `serialize:"true"`
}

// Attempts to verify this transaction with the provided state.
func (tx *StatefulAdvanceTimeTx) SemanticVerify(vm *VM, parentState MutableState, stx *signed.Tx) error {
	_, _, err := tx.Execute(vm, parentState, stx)
	return err
}

// Execute this transaction.
func (tx *StatefulAdvanceTimeTx) Execute(
	vm *VM,
	parentState MutableState,
	stx *signed.Tx,
) (
	VersionedState,
	VersionedState,
	error,
) {
	switch {
	case tx == nil:
		return nil, nil, unsigned.ErrNilTx
	case len(stx.Creds) != 0:
		return nil, nil, unsigned.ErrWrongNumberOfCredentials
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
	nextStakerChangeTime, err := getNextStakerChangeTime(parentState)
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
	toAddWithoutRewardToCurrent := []*signed.Tx(nil)
	numToRemoveFromPending := 0

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp. [pendingStakers.Stakers()] is sorted in order
	// of increasing startTime
pendingStakerLoop:
	for _, tx := range pendingStakers.Stakers() {
		switch staker := tx.Unsigned.(type) {
		case *unsigned.AddDelegatorTx:
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
		case *unsigned.AddValidatorTx:
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
		case *unsigned.AddSubnetValidatorTx:
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
			return nil, nil, fmt.Errorf("expected validator but got %T", tx.Unsigned)
		}
	}
	newlyPendingStakers := pendingStakers.DeleteStakers(numToRemoveFromPending)

	currentStakers := parentState.CurrentStakerChainState()
	numToRemoveFromCurrent := 0

	// Remove from the staker set any subnet validators whose endTime is at or
	// before the new timestamp
currentStakerLoop:
	for _, tx := range currentStakers.Stakers() {
		switch staker := tx.Unsigned.(type) {
		case *unsigned.AddSubnetValidatorTx:
			if staker.EndTime().After(txTimestamp) {
				break currentStakerLoop
			}

			numToRemoveFromCurrent++
		case *unsigned.AddValidatorTx, *unsigned.AddDelegatorTx:
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
func (tx *StatefulAdvanceTimeTx) InitiallyPrefersCommit(vm *VM) bool {
	txTimestamp := tx.Timestamp()
	localTimestamp := vm.clock.Time()
	localTimestampPlusSync := localTimestamp.Add(syncBound)
	return !txTimestamp.After(localTimestampPlusSync)
}

// newAdvanceTimeTx creates a new tx that, if it is accepted and followed by a
// Commit block, will set the chain's timestamp to [timestamp].
func (vm *VM) newAdvanceTimeTx(timestamp time.Time) (*signed.Tx, error) {
	utx := &unsigned.AdvanceTimeTx{Time: uint64(timestamp.Unix())}
	tx, err := signed.NewSigned(utx, unsigned.Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(vm.ctx)
}
