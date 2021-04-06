// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

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
	parentState versionedState,
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
	nextStakerChangeTime, err := vm.nextStakerChangeTime(parentState)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	newTimestamp := tx.Timestamp()
	if newTimestamp.After(nextStakerChangeTime) {
		return nil, nil, nil, nil, permError{
			fmt.Errorf(
				"proposed timestamp (%s) later than next staker change time (%s)",
				newTimestamp,
				nextStakerChangeTime,
			),
		}
	}

	// Specify what the state of the chain will be if this proposal is committed
	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()
	onCommitState := NewVersionedState(parentState, currentStakers, pendingStakers)
	onCommitState.SetTimestamp(newTimestamp)

	if err := vm.updateValidators(onCommitState); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// If this block is committed, update the validator sets.
	// onCommitDB will be committed to vm.DB before this is called.
	onCommitFunc := func() error {
		// For each Subnet, update the node's validator manager to reflect
		// current Subnet membership
		return vm.updateVdrMgr(false)
	}

	// State doesn't change if this proposal is aborted
	return onCommitState, parentState, onCommitFunc, nil, nil
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
