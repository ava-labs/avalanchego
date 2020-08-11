// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/verify"

	safemath "github.com/ava-labs/gecko/utils/math"
)

var (
	errShouldBeDSValidator = errors.New("expected validator to be in the default subnet")
	errOverflowReward      = errors.New("overflow while calculating validator reward")
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
	vm *VM
	// ID of this tx
	id ids.ID
	// Byte representation of this unsigned tx
	unsignedBytes []byte
	// Byte representation of the signed transaction (ie with credentials)
	bytes []byte

	// ID of the tx that created the delegator/validator being removed/rewarded
	TxID ids.ID `serialize:"true" json:"txID"`
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *UnsignedRewardValidatorTx) initialize(vm *VM, bytes []byte) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedRewardValidatorTx: %w", err)
	}
	return nil
}

// ID returns the ID of this transaction
func (tx *UnsignedRewardValidatorTx) ID() ids.ID { return tx.id }

// SyntacticVerify that this transaction is well formed
func (tx *UnsignedRewardValidatorTx) SyntacticVerify() TxError {
	switch {
	case tx == nil:
		return tempError{errNilTx}
	case tx.TxID.IsZero():
		return tempError{errInvalidID}
	default:
		return nil
	}
}

var (
	errWrongTxType = errors.New("wrong transaction type")
)

// SemanticVerify this transaction performs a valid state transition.
//
// The current validating set must have at least one member.
// The next validator to be removed must be the validator specified in this block.
// The next validator to be removed must be have an end time equal to the current
//   chain timestamp.
func (tx *UnsignedRewardValidatorTx) SemanticVerify(
	db database.Database,
	stx *ProposalTx,
) (
	*versiondb.Database,
	*versiondb.Database,
	func() error,
	func() error,
	TxError,
) {
	if len(stx.Credentials) != 0 {
		return nil, nil, nil, nil, permError{errWrongNumberOfCredentials}
	}
	if err := tx.SyntacticVerify(); err != nil {
		return nil, nil, nil, nil, err
	}

	defaultSubnetVdrHeap, err := tx.vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if defaultSubnetVdrHeap.Len() == 0 { // there is no validator to remove
		return nil, nil, nil, nil, permError{errEmptyValidatingSet}
	}

	vdrTx := defaultSubnetVdrHeap.Remove()
	txID := vdrTx.ID()
	if !txID.Equals(tx.TxID) {
		return nil, nil, nil, nil, permError{fmt.Errorf("attempting to remove TxID: %s. Should be removing %s",
			tx.TxID,
			txID)}
	}

	// Verify that the chain's timestamp is the validator's end time
	currentTime, err := tx.vm.getTimestamp(db)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	unsignedVdrTx, ok := vdrTx.UnsignedProposalTx.(TimedTx)
	if !ok {
		return nil, nil, nil, nil, permError{errWrongTxType}
	}
	if endTime := unsignedVdrTx.EndTime(); !endTime.Equal(currentTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			endTime)}
	}

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

	switch uVdrTx := vdrTx.UnsignedProposalTx.(type) {
	case *UnsignedAddDefaultSubnetValidatorTx:
		// Refund the stake here
		for i, out := range uVdrTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(uVdrTx.Outs) + i),
				},
				Asset: avax.Asset{ID: tx.vm.avaxAssetID},
				Out:   out.Output(),
			}

			if err := tx.vm.putUTXO(onCommitDB, utxo); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
			if err := tx.vm.putUTXO(onAbortDB, utxo); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
		}

		// Provide the reward here
		if reward := reward(uVdrTx.Duration(), uVdrTx.Wght, InflationRate); reward > 0 {
			outIntf, err := tx.vm.fx.CreateOutput(reward, uVdrTx.RewardsOwner)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, nil, nil, permError{errInvalidState}
			}
			if err := tx.vm.putUTXO(onCommitDB, &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(uVdrTx.Outs) + len(uVdrTx.Stake)),
				},
				Asset: avax.Asset{ID: tx.vm.avaxAssetID},
				Out:   out,
			}); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
		}
	case *UnsignedAddDefaultSubnetDelegatorTx:
		// We're removing a delegator
		parentTx, err := defaultSubnetVdrHeap.getDefaultSubnetStaker(uVdrTx.NodeID)
		if err != nil {
			return nil, nil, nil, nil, permError{err}
		}
		unsignedParentTx := parentTx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx)

		// Refund the stake here
		for i, out := range uVdrTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(uVdrTx.Outs) + i),
				},
				Asset: avax.Asset{ID: tx.vm.avaxAssetID},
				Out:   out.Output(),
			}

			if err := tx.vm.putUTXO(onCommitDB, utxo); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
			if err := tx.vm.putUTXO(onAbortDB, utxo); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
		}

		// If reward given, it will be this amount
		reward := reward(uVdrTx.Duration(), uVdrTx.Wght, InflationRate)
		// Calculate split of reward between delegator/delegatee
		// The delegator gives stake to the validatee
		delegatorShares := NumberOfShares - uint64(unsignedParentTx.Shares) // parentTx.Shares <= NumberOfShares so no underflow
		delegatorReward := delegatorShares * (reward / NumberOfShares)      // delegatorShares <= NumberOfShares so no overflow
		// Delay rounding as long as possible for small numbers
		if optimisticReward, err := safemath.Mul64(delegatorShares, reward); err == nil {
			delegatorReward = optimisticReward / NumberOfShares
		}
		delegateeReward := reward - delegatorReward // delegatorReward <= reward so no underflow

		offset := 0

		// Reward the delegator here
		if delegatorReward > 0 {
			outIntf, err := tx.vm.fx.CreateOutput(reward, uVdrTx.RewardsOwner)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, nil, nil, permError{errInvalidState}
			}
			if err := tx.vm.putUTXO(onCommitDB, &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(uVdrTx.Outs) + len(uVdrTx.Stake)),
				},
				Asset: avax.Asset{ID: tx.vm.avaxAssetID},
				Out:   out,
			}); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}

			offset++
		}

		// Reward the delegatee here
		if delegateeReward > 0 {
			outIntf, err := tx.vm.fx.CreateOutput(reward, unsignedParentTx.RewardsOwner)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, nil, nil, permError{errInvalidState}
			}
			if err := tx.vm.putUTXO(onCommitDB, &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(uVdrTx.Outs) + len(uVdrTx.Stake) + offset),
				},
				Asset: avax.Asset{ID: tx.vm.avaxAssetID},
				Out:   out,
			}); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
		}
	default:
		return nil, nil, nil, nil, permError{errShouldBeDSValidator}
	}

	// Regardless of whether this tx is committed or aborted, update the
	// validator set to remove the staker. onAbortDB or onCommitDB should commit
	// (flush to vm.DB) before this is called
	updateValidators := func() error {
		return tx.vm.updateValidators(constants.DefaultSubnetID)
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
func (tx *UnsignedRewardValidatorTx) InitiallyPrefersCommit() bool { return true }

// RewardStakerTx creates a new transaction that proposes to remove the staker
// [validatorID] from the default validator set.
func (vm *VM) newRewardValidatorTx(txID ids.ID) (*ProposalTx, error) {
	tx := &ProposalTx{UnsignedProposalTx: &UnsignedRewardValidatorTx{
		TxID: txID,
	}}

	txBytes, err := vm.codec.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}
	return tx, tx.initialize(vm, txBytes)
}
