// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"container/heap"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	safemath "github.com/ava-labs/gecko/utils/math"
)

var (
	errShouldBeDSValidator = errors.New("expected validator to be in the default subnet")
	errOverflowReward      = errors.New("overflow while calculating validator reward")
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
	vm   *VM
}

// initialize this tx
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

	defaultSubnetVdrHeap, err := tx.vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
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
		return nil, nil, nil, nil, tempError{err}
	} else if endTime := vdrTx.EndTime(); !endTime.Equal(currentTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			endTime)}
	}

	heap.Pop(defaultSubnetVdrHeap) // Remove validator from the validator set

	// If this tx's proposal is committed, remove the validator from the validator set
	onCommitDB := versiondb.New(db)
	if err := tx.vm.putCurrentValidators(onCommitDB, defaultSubnetVdrHeap, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// If this tx's proposal is aborted, remove the validator from the validator set
	onAbortDB := versiondb.New(db)
	if err := tx.vm.putCurrentValidators(onAbortDB, defaultSubnetVdrHeap, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// [utxo] will be copied and modified below. It's a boilerplate.
	// Cleaner than re-declaring this struct many times.
	utxo := ava.UTXO{
		UTXOID: ava.UTXOID{
			TxID:        vdrTx.ID(), // Points to the tx that added the validator
			OutputIndex: 0,          // Will be modified
		},
		Asset: ava.Asset{ID: tx.vm.avaxAssetID},
		Out:   nil, // Will be modified
	}
	out := secp256k1fx.TransferOutput{ // Also a boilerplate, like [utxo]
		Amt: 0, // Will be modified
		OutputOwners: secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     nil, // Will be modified
		},
	}

	switch vdrTx := vdrTx.(type) {
	case *addDefaultSubnetValidatorTx:
		// We're removing a default subnet validator
		// Calculate the reward
		reward := reward(vdrTx.Duration(), vdrTx.Wght, InflationRate)
		amountWithReward, err := safemath.Add64(vdrTx.Wght, reward) // overflow should never happen
		if err != nil {
			amountWithReward = math.MaxUint64
			tx.vm.Ctx.Log.Error(errOverflowReward.Error()) // This should never happen
		}

		// If this proposal is committed, they get the reward
		commitUTXO := utxo // Copies the struct (_not_ a reference)
		commitUTXO.OutputIndex = uint32(len(vdrTx.Outs()))
		commitOut := out // Copies the struct (_not_ a reference)
		commitOut.Addrs = []ids.ShortID{vdrTx.Destination}
		commitOut.Amt = amountWithReward
		commitUTXO.Out = &commitOut
		if err := tx.vm.putUTXO(onCommitDB, &commitUTXO); err != nil {
			return nil, nil, nil, nil, tempError{err}
		}

		// If this proposal is rejected, they just get back staked tokens
		abortUTXO := utxo // Copies the struct (_not_ a reference)
		abortUTXO.OutputIndex = uint32(len(vdrTx.Outs()))
		abortOut := out // Copies the struct (_not_ a reference)
		abortOut.Addrs = []ids.ShortID{vdrTx.Destination}
		abortOut.Amt = vdrTx.Wght // No reward --> just get staked AVAX back
		abortUTXO.Out = &abortOut
		if err := tx.vm.putUTXO(onAbortDB, &abortUTXO); err != nil {
			return nil, nil, nil, nil, tempError{err}
		}
	case *addDefaultSubnetDelegatorTx:
		// We're removing a delegator
		parentTx, err := defaultSubnetVdrHeap.getDefaultSubnetStaker(vdrTx.NodeID)
		if err != nil {
			return nil, nil, nil, nil, permError{err}
		}

		// If reward given, it will be this amount
		reward := reward(vdrTx.Duration(), vdrTx.Wght, InflationRate)
		// Calculate split of reward between delegator/delegatee
		// The delegator gives stake to the validatee
		delegatorShares := NumberOfShares - uint64(parentTx.Shares)    // parentTx.Shares <= NumberOfShares so no underflow
		delegatorReward := delegatorShares * (reward / NumberOfShares) // delegatorShares <= NumberOfShares so no overflow
		// Delay rounding as long as possible for small numbers
		if optimisticReward, err := safemath.Mul64(delegatorShares, reward); err == nil {
			delegatorReward = optimisticReward / NumberOfShares
		}
		delegateeReward := reward - delegatorReward // delegatorReward <= reward so no underflow

		// Record the amount the delegator/delegatee receive if a reward is given
		delegatorAmtWithReward, err := safemath.Add64(vdrTx.Wght, delegatorReward) // overflow should never happen
		if err != nil {
			delegatorAmtWithReward = math.MaxUint64
			tx.vm.Ctx.Log.Error(errOverflowReward.Error())
		}
		delegatorCommitUTXO := utxo // Copies the struct (_not_ a reference)
		delegatorCommitUTXO.OutputIndex = uint32(len(vdrTx.Outs()))
		delegatorCommitOut := out // Copies the struct (_not_ a reference)
		delegatorCommitOut.Addrs = []ids.ShortID{vdrTx.Destination}
		delegatorCommitOut.Amt = delegatorAmtWithReward // Delegator gets back their stake plus reward
		delegatorCommitUTXO.Out = &delegatorCommitOut
		if err := tx.vm.putUTXO(onCommitDB, &delegatorCommitUTXO); err != nil {
			return nil, nil, nil, nil, tempError{err}
		}

		delegateeCommitUTXO := utxo // Copies the struct (_not_ a reference)
		delegateeCommitUTXO.OutputIndex = delegatorCommitUTXO.OutputIndex + 1
		delegateeCommitOut := out // Copies the struct (_not_ a reference)
		delegateeCommitOut.Addrs = []ids.ShortID{parentTx.Destination}
		delegateeCommitOut.Amt = delegateeReward // Delegatee gets their reward share
		delegateeCommitUTXO.Out = &delegateeCommitOut
		if err := tx.vm.putUTXO(onCommitDB, &delegateeCommitUTXO); err != nil {
			return nil, nil, nil, nil, tempError{err}
		}

		// If no reward is given, just give delegatee their AVAX back
		abortUTXO := utxo // Copies the struct (_not_ a reference)
		abortUTXO.OutputIndex = uint32(len(vdrTx.Outs()))
		abortOut := out
		abortOut.Addrs = []ids.ShortID{vdrTx.Destination}
		abortOut.Amt = vdrTx.Wght // No reward --> just get staked AVAX back
		abortUTXO.Out = &abortOut
		if err := tx.vm.putUTXO(onAbortDB, &abortUTXO); err != nil {
			return nil, nil, nil, nil, tempError{err}
		}
	default:
		return nil, nil, nil, nil, permError{errShouldBeDSValidator}
	}

	// Regardless of whether this tx is committed or aborted, update the
	// validator set to remove the staker. onAbortDB or onCommitDB should commit
	// (flush to vm.DB) before this is called
	updateValidators := func() {
		if err := tx.vm.updateValidators(constants.DefaultSubnetID); err != nil {
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
