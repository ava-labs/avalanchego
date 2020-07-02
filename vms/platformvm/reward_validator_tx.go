// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"container/heap"
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
)

var (
	errShouldBeDSValidator = errors.New("expected validator to be in the default subnet")
)

// rewardValidatorTx is a transaction that represents a proposal to remove a
// validator that is currently validating from the validator set.
//
// If this transaction is accepted and the next block accepted is a Commit
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX as well as a validating reward.
//
// If this transaction is accepted and the next block accepted is an Abort
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX but no reward.
type rewardValidatorTx struct {
	// ID of the tx that created the delegator/validator being removed/rewarded
	TxID ids.ID `serialize:"true"`

	vm *VM
}

func (tx *rewardValidatorTx) initialize(vm *VM) error {
	tx.vm = vm
	return nil
}

// SyntacticVerify that this transaction is well formed
func (tx *rewardValidatorTx) SyntacticVerify() TxError {
	switch {
	case tx == nil:
		return tempError{errNilTx}
	case tx.TxID.IsZero():
		return tempError{errInvalidID}
	default:
		return nil
	}
}

// SemanticVerify this transaction performs a valid state transition.
//
// The current validating set must have at least one member.
// The next validator to be removed must be the validator specified in this block.
// The next validator to be removed must be have an end time equal to the current
//   chain timestamp.
func (tx *rewardValidatorTx) SemanticVerify(db database.Database) (*versiondb.Database, *versiondb.Database, func(), func(), TxError) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, err
	} else if db == nil {
		return nil, nil, nil, nil, tempError{errDBNil}
	}

	defaultSubnetVdrHeap, err := tx.vm.getCurrentValidators(db, DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{errDBCurrentValidators}
	} else if defaultSubnetVdrHeap.Len() == 0 { // there is no validator to remove
		return nil, nil, nil, nil, permError{errEmptyValidatingSet}
	}

	vdrTx := defaultSubnetVdrHeap.Peek()

	if txID := vdrTx.ID(); !txID.Equals(tx.TxID) {
		return nil, nil, nil, nil, permError{fmt.Errorf("attempting to remove TxID: %s. Should be removing %s",
			tx.TxID,
			txID)}
	}

	// Verify that the chain's timestamp is the validator's end time
	currentTime, err := tx.vm.getTimestamp(db)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	if endTime := vdrTx.EndTime(); !endTime.Equal(currentTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			endTime)}
	}

	heap.Pop(defaultSubnetVdrHeap) // Remove validator from the validator set

	onCommitDB := versiondb.New(db)
	// If this tx's proposal is committed, remove the validator from the validator set and update the
	// account balance to reflect the return of staked AVAX and their reward.
	if err := tx.vm.putCurrentValidators(onCommitDB, defaultSubnetVdrHeap, DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{errDBPutCurrentValidators}
	}

	onAbortDB := versiondb.New(db)
	// If this tx's proposal is aborted, remove the validator from the validator set and update the
	// account balance to reflect the return of staked AVAX
	if err := tx.vm.putCurrentValidators(onAbortDB, defaultSubnetVdrHeap, DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{errDBPutCurrentValidators}
	}

	/* TODO
	switch vdrTx := vdrTx.(type) {
	case *addDefaultSubnetValidatorTx:
			duration := vdrTx.Duration()
			stakedAmount := vdrTx.Wght
			reward := reward(duration, stakedAmount, InflationRate)
			amountWithReward, err := math.Add64(stakedAmount, reward)
			if err != nil {
				amountWithReward = stakedAmount
				tx.vm.Ctx.Log.Error("error while calculating balance + reward: %s", err)
			}
			destination := vdrTx.Destination
			// TODO create UTXO that rewards this validator
			// It should probably be part of the tx itself
		return nil, nil, nil, nil, tempError{errors.New("TODO")}
	case *addDefaultSubnetDelegatorTx:
			parentTx, err := defaultSubnetVdrHeap.getDefaultSubnetStaker(vdrTx.NodeID)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}

			duration := vdrTx.Duration()
			amount := vdrTx.Wght
			reward := reward(duration, amount, InflationRate)

			// Because parentTx.Shares <= NumberOfShares this will never underflow
			delegatorShares := NumberOfShares - uint64(parentTx.Shares)
			// Because delegatorShares <= NumberOfShares this will never overflow
			delegatorReward := delegatorShares * (reward / NumberOfShares)
			// Delay rounding as long as possible for small numbers
			if optimisticReward, err := math.Mul64(delegatorShares, reward); err == nil {
				delegatorReward = optimisticReward / NumberOfShares
			}

			// Because delegatorReward <= reward this will never underflow
			//validatorReward := reward - delegatorReward

			// TODO 		// TODO create UTXO that rewards this validator
			// It should probably be part of the tx itself
		return nil, nil, nil, nil, tempError{errors.New("TODO")}
	default:
		return nil, nil, nil, nil, permError{errShouldBeDSValidator}
	}
	*/

	// Regardless of whether this tx is committed or aborted, update the
	// validator set to remove the staker. onAbortDB or onCommitDB should commit
	// (flush to vm.DB) before this is called
	updateValidators := func() {
		if err := tx.vm.updateValidators(DefaultSubnetID); err != nil {
			tx.vm.Ctx.Log.Fatal("failed to update validators on the default subnet: %s", err)
		}
	}

	return onCommitDB, onAbortDB, updateValidators, updateValidators, nil
}

// InitiallyPrefersCommit returns true.
//
// Right now, *Commit (that is, remove the validator and reward them) is always
// preferred over *Abort (remove the validator but don't reward them.)
//
// TODO: A validator should receive a reward only if they are sufficiently
// responsive and correct during the time they are validating.
func (tx *rewardValidatorTx) InitiallyPrefersCommit() bool { return true }

// RewardStakerTx creates a new transaction that proposes to remove the staker
// [validatorID] from the default validator set.
func (vm *VM) newRewardValidatorTx(txID ids.ID) (*rewardValidatorTx, error) {
	tx := &rewardValidatorTx{
		TxID: txID,
	}
	return tx, tx.initialize(vm)
}
