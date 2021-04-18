// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	_ UnsignedProposalTx = &UnsignedAdvanceTimeTx{}
)

// UnsignedAdvanceTimeTx is a transaction to increase the chain's timestamp.
// When the chain's timestamp is updated (a AdvanceTimeTx is accepted and
// followed by a commit block) the staker set is also updated accordingly.
// It must be that:
//   * proposed timestamp > [current chain time]
//   * proposed timestamp <= [time for next staker to be removed]
type UnsignedAdvanceTimeTx struct {
	avax.Metadata

	// Unix time this block proposes increasing the timestamp to
	Time uint64 `serialize:"true" json:"time"`
}

// Timestamp returns the time this block is proposing the chain should be set to
func (tx *UnsignedAdvanceTimeTx) Timestamp() time.Time {
	return time.Unix(int64(tx.Time), 0)
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAdvanceTimeTx) SemanticVerify(
	vm *VM,
	parentState mutableState,
	stx *Tx,
) (
	versionedState,
	versionedState,
	func() error,
	func() error,
	TxError,
) {
	switch {
	case tx == nil:
		return nil, nil, nil, nil, tempError{errNilTx}
	case vm.clock.Time().Add(syncBound).Before(tx.Timestamp()):
		return nil, nil, nil, nil, tempError{
			fmt.Errorf(
				"proposed time, %s, is too far in the future relative to local time (%s)",
				tx.Timestamp(),
				vm.clock.Time(),
			),
		}
	case len(stx.Creds) != 0:
		return nil, nil, nil, nil, permError{errWrongNumberOfCredentials}
	}

	currentTimestamp := parentState.GetTimestamp()
	if tx.Time <= uint64(currentTimestamp.Unix()) {
		return nil, nil, nil, nil, permError{
			fmt.Errorf(
				"proposed timestamp (%s), not after current timestamp (%s)",
				tx.Timestamp(),
				currentTimestamp,
			),
		}
	}

	// Only allow timestamp to move forward as far as the time of next staker set change time
	nextStakerRemovalTime, err := vm.nextStakerRemovalTime(parentState)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	timestamp := tx.Timestamp()
	if timestamp.After(nextStakerRemovalTime) {
		return nil, nil, nil, nil, permError{
			fmt.Errorf(
				"proposed timestamp (%s) later than next staker removal time (%s)",
				timestamp,
				nextStakerRemovalTime,
			),
		}
	}

	currentSupply := parentState.GetCurrentSupply()

	pendingStakers := parentState.PendingStakerChainState()
	toAddWithRewardToCurrent := []*validatorReward(nil)
	toAddWithoutRewardToCurrent := []*Tx(nil)
	numToRemoveFromPending := 0

pendingStakerLoop:
	for _, tx := range pendingStakers.Stakers() {
		switch staker := tx.UnsignedTx.(type) {
		case *UnsignedAddDelegatorTx:
			if staker.StartTime().After(timestamp) {
				break pendingStakerLoop
			}

			reward := Reward(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
				vm.StakeMintingPeriod,
			)
			currentSupply, err = safemath.Add64(currentSupply, reward)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}

			toAddWithRewardToCurrent = append(toAddWithRewardToCurrent, &validatorReward{
				addStakerTx:     tx,
				potentialReward: reward,
			})
			numToRemoveFromPending++
		case *UnsignedAddValidatorTx:
			if staker.StartTime().After(timestamp) {
				break pendingStakerLoop
			}

			reward := Reward(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
				vm.StakeMintingPeriod,
			)
			currentSupply, err = safemath.Add64(currentSupply, reward)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}

			toAddWithRewardToCurrent = append(toAddWithRewardToCurrent, &validatorReward{
				addStakerTx:     tx,
				potentialReward: reward,
			})
			numToRemoveFromPending++
		case *UnsignedAddSubnetValidatorTx:
			if staker.StartTime().After(timestamp) {
				break pendingStakerLoop
			}

			// If this staker should already be removed, then we should just
			// never add them.
			if staker.EndTime().After(timestamp) {
				toAddWithoutRewardToCurrent = append(toAddWithoutRewardToCurrent, tx)
			}
			numToRemoveFromPending++
		default:
			return nil, nil, nil, nil, permError{
				fmt.Errorf("expected validator but got %T", tx.UnsignedTx),
			}
		}
	}
	newlyPendingStakers := pendingStakers.DeleteStaker(numToRemoveFromPending)

	currentStakers := parentState.CurrentStakerChainState()
	numToRemoveFromCurrent := 0

currentStakerLoop:
	for _, tx := range currentStakers.Stakers() {
		switch staker := tx.UnsignedTx.(type) {
		case *UnsignedAddSubnetValidatorTx:
			if staker.EndTime().After(timestamp) {
				break currentStakerLoop
			}

			numToRemoveFromCurrent++
		default:
			// We shouldn't be removing any primary network validators here
			break currentStakerLoop
		}
	}
	newlyCurrentStakers, err := currentStakers.UpdateStakers(
		toAddWithRewardToCurrent,
		toAddWithoutRewardToCurrent,
		numToRemoveFromCurrent,
	)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	onCommitState := NewVersionedState(parentState, newlyCurrentStakers, newlyPendingStakers)
	onCommitState.SetTimestamp(timestamp)
	onCommitState.SetCurrentSupply(currentSupply)

	onAbortState := NewVersionedState(parentState, currentStakers, pendingStakers)

	// If this block is committed, update the validator sets.
	// onCommitDB will be committed to vm.DB before this is called.
	onCommitFunc := func() error {
		// For each Subnet, update the node's validator manager to reflect
		// current Subnet membership
		return vm.updateValidators(false)
	}

	// State doesn't change if this proposal is aborted
	return onCommitState, onAbortState, onCommitFunc, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed time is at
// or before the current time plus the synchrony bound
func (tx *UnsignedAdvanceTimeTx) InitiallyPrefersCommit(vm *VM) bool {
	return !tx.Timestamp().After(vm.clock.Time().Add(syncBound))
}

// newAdvanceTimeTx creates a new tx that, if it is accepted and followed by a
// Commit block, will set the chain's timestamp to [timestamp].
func (vm *VM) newAdvanceTimeTx(timestamp time.Time) (*Tx, error) {
	tx := &Tx{UnsignedTx: &UnsignedAdvanceTimeTx{
		Time: uint64(timestamp.Unix()),
	}}
	return tx, tx.Sign(vm.codec, nil)
}
