// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	txstate "github.com/ava-labs/avalanchego/vms/platformvm/state/transactions"
)

var _ ProposalTx = &AdvanceTimeTx{}

// SyncBound is the synchrony bound used for safe decision making
const SyncBound = 10 * time.Second

type AdvanceTimeTx struct {
	*unsigned.AdvanceTimeTx

	ID    ids.ID // ID of signed advance time tx
	creds []verify.Verifiable
}

// Attempts to verify this transaction with the provided state.
func (tx *AdvanceTimeTx) SemanticVerify(
	verifier TxVerifier,
	parentState state.Mutable,
) error {
	_, _, err := tx.Execute(verifier, parentState)
	return err
}

// Execute this transaction.
func (tx *AdvanceTimeTx) Execute(
	verifier TxVerifier,
	parentState state.Mutable,
) (
	state.Versioned,
	state.Versioned,
	error,
) {
	clock := verifier.Clock()

	switch {
	case tx == nil:
		return nil, nil, unsigned.ErrNilTx
	case len(tx.creds) != 0:
		return nil, nil, unsigned.ErrWrongNumberOfCredentials
	}

	txTimestamp := tx.Timestamp()
	localTimestamp := clock.Time()
	localTimestampPlusSync := localTimestamp.Add(SyncBound)
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
	nextStakerChangeTime, err := parentState.GetNextStakerChangeTime()
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
	toAddValidatorsWithRewardToCurrent := []*txstate.ValidatorReward(nil)
	toAddDelegatorsWithRewardToCurrent := []*txstate.ValidatorReward(nil)
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

			r := verifier.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = safemath.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, err
			}

			toAddDelegatorsWithRewardToCurrent = append(toAddDelegatorsWithRewardToCurrent, &txstate.ValidatorReward{
				AddStakerTx:     tx,
				PotentialReward: r,
			})
			numToRemoveFromPending++
		case *unsigned.AddValidatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			r := verifier.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = safemath.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, err
			}

			toAddValidatorsWithRewardToCurrent = append(toAddValidatorsWithRewardToCurrent, &txstate.ValidatorReward{
				AddStakerTx:     tx,
				PotentialReward: r,
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
			return nil, nil, fmt.Errorf("expected tx type *unsigned.AddValidatorTx or *unsigned.AddDelegatorTx but got %T", staker)
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

	onCommitState := state.NewVersioned(parentState, newlyCurrentStakers, newlyPendingStakers)
	onCommitState.SetTimestamp(txTimestamp)
	onCommitState.SetCurrentSupply(currentSupply)

	// State doesn't change if this proposal is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)

	return onCommitState, onAbortState, nil
}

// InitiallyPrefersCommit returns true if the proposed time is at
// or before the current time plus the synchrony bound
func (tx *AdvanceTimeTx) InitiallyPrefersCommit(verifier TxVerifier) bool {
	clock := verifier.Clock()
	txTimestamp := tx.Timestamp()
	localTimestamp := clock.Time()
	localTimestampPlusSync := localTimestamp.Add(SyncBound)
	return !txTimestamp.After(localTimestampPlusSync)
}
