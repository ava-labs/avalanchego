// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanche-go/database"
	"github.com/ava-labs/avalanche-go/database/versiondb"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils/constants"
	"github.com/ava-labs/avalanche-go/vms/components/avax"
	"github.com/ava-labs/avalanche-go/vms/components/verify"

	safemath "github.com/ava-labs/avalanche-go/utils/math"
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

// SemanticVerify this transaction performs a valid state transition.
//
// The current validating set must have at least one member.
// The next validator to be removed must be the validator specified in this block.
// The next validator to be removed must be have an end time equal to the current
//   chain timestamp.
func (tx *UnsignedRewardValidatorTx) SemanticVerify(
	vm *VM,
	db database.Database,
	stx *Tx,
) (
	*versiondb.Database,
	*versiondb.Database,
	func() error,
	func() error,
	TxError,
) {
	switch {
	case tx == nil:
		return nil, nil, nil, nil, tempError{errNilTx}
	case tx.TxID.IsZero():
		return nil, nil, nil, nil, tempError{errInvalidID}
	case len(stx.Creds) != 0:
		return nil, nil, nil, nil, permError{errWrongNumberOfCredentials}
	}

	stakerTx, err := vm.nextStakerStop(db, constants.PrimaryNetworkID)
	if err != nil {
		return nil, nil, nil, nil, permError{err}
	}
	if stakerID := stakerTx.Tx.ID(); !stakerID.Equals(tx.TxID) {
		return nil, nil, nil, nil, permError{fmt.Errorf("attempting to remove TxID: %s. Should be removing %s",
			tx.TxID,
			stakerID)}
	}

	// Verify that the chain's timestamp is the validator's end time
	currentTime, err := vm.getTimestamp(db)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	staker, ok := stakerTx.Tx.UnsignedTx.(TimedTx)
	if !ok {
		return nil, nil, nil, nil, permError{errWrongTxType}
	}
	if endTime := staker.EndTime(); !endTime.Equal(currentTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			endTime)}
	}

	// If this tx's proposal is committed, remove the validator from the validator set
	onCommitDB := versiondb.New(db)
	if err := vm.removeStaker(onCommitDB, constants.PrimaryNetworkID, stakerTx); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// If this tx's proposal is aborted, remove the validator from the validator set
	onAbortDB := versiondb.New(db)
	if err := vm.removeStaker(onAbortDB, constants.PrimaryNetworkID, stakerTx); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	nodeID := ids.ShortID{}
	startTime := time.Time{}
	switch uStakerTx := stakerTx.Tx.UnsignedTx.(type) {
	case *UnsignedAddValidatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out:   out.Output(),
			}

			if err := vm.putUTXO(onCommitDB, utxo); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
			if err := vm.putUTXO(onAbortDB, utxo); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
		}

		// Provide the reward here
		if stakerTx.Reward > 0 {
			outIntf, err := vm.fx.CreateOutput(stakerTx.Reward, uStakerTx.RewardsOwner)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, nil, nil, permError{errInvalidState}
			}
			if err := vm.putUTXO(onCommitDB, &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out:   out,
			}); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}

			currentSupply, err := vm.getCurrentSupply(onAbortDB)
			if err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
			newSupply, err := safemath.Sub64(currentSupply, stakerTx.Reward)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			if err := vm.putCurrentSupply(onAbortDB, newSupply); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
		}

		// Handle reward preferences
		nodeID = uStakerTx.Validator.ID()
		startTime = uStakerTx.StartTime()
		if err := vm.deleteUptime(onCommitDB, nodeID); err != nil {
			return nil, nil, nil, nil, tempError{err}
		}
		if err := vm.deleteUptime(onAbortDB, nodeID); err != nil {
			return nil, nil, nil, nil, tempError{err}
		}
	case *UnsignedAddDelegatorTx:
		// We're removing a delegator
		vdrTx, ok, err := vm.isValidator(db, constants.PrimaryNetworkID, uStakerTx.Validator.NodeID)
		if err != nil {
			return nil, nil, nil, nil, tempError{err}
		}
		if !ok {
			return nil, nil, nil, nil, permError{
				fmt.Errorf("couldn't find validator %s: %w", uStakerTx.Validator.NodeID, err)}
		}
		vdr, ok := vdrTx.(*UnsignedAddValidatorTx)
		if !ok {
			return nil, nil, nil, nil, permError{
				fmt.Errorf("expected vdr to be *UnsignedAddValidatorTx but is %T", vdrTx)}
		}

		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out:   out.Output(),
			}

			if err := vm.putUTXO(onCommitDB, utxo); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
			if err := vm.putUTXO(onAbortDB, utxo); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
		}

		currentSupply, err := vm.getCurrentSupply(onAbortDB)
		if err != nil {
			return nil, nil, nil, nil, tempError{err}
		}
		newSupply, err := safemath.Sub64(currentSupply, stakerTx.Reward)
		if err != nil {
			return nil, nil, nil, nil, permError{err}
		}
		if err := vm.putCurrentSupply(onAbortDB, newSupply); err != nil {
			return nil, nil, nil, nil, tempError{err}
		}

		// Calculate split of reward between delegator/delegatee
		// The delegator gives stake to the validatee
		delegatorShares := PercentDenominator - uint64(vdr.Shares)                  // parentTx.Shares <= NumberOfShares so no underflow
		delegatorReward := delegatorShares * (stakerTx.Reward / PercentDenominator) // delegatorShares <= NumberOfShares so no overflow
		// Delay rounding as long as possible for small numbers
		if optimisticReward, err := safemath.Mul64(delegatorShares, stakerTx.Reward); err == nil {
			delegatorReward = optimisticReward / PercentDenominator
		}
		delegateeReward := stakerTx.Reward - delegatorReward // delegatorReward <= reward so no underflow

		offset := 0

		// Reward the delegator here
		if delegatorReward > 0 {
			outIntf, err := vm.fx.CreateOutput(delegatorReward, uStakerTx.RewardsOwner)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, nil, nil, permError{errInvalidState}
			}
			if err := vm.putUTXO(onCommitDB, &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out:   out,
			}); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}

			offset++
		}

		// Reward the delegatee here
		if delegateeReward > 0 {
			outIntf, err := vm.fx.CreateOutput(delegateeReward, vdr.RewardsOwner)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, nil, nil, permError{errInvalidState}
			}
			if err := vm.putUTXO(onCommitDB, &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake) + offset),
				},
				Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
				Out:   out,
			}); err != nil {
				return nil, nil, nil, nil, tempError{err}
			}
		}
		nodeID = uStakerTx.Validator.ID()
		startTime = vdrTx.StartTime()
	default:
		return nil, nil, nil, nil, permError{errShouldBeDSValidator}
	}

	// Regardless of whether this tx is committed or aborted, update the
	// validator set to remove the staker. onAbortDB or onCommitDB should commit
	// (flush to vm.DB) before this is called
	updateValidators := func() error { return vm.updateVdrMgr(false) }

	uptime, err := vm.calculateUptime(vm.DB, nodeID, startTime)
	if err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	tx.shouldPreferCommit = uptime >= vm.uptimePercentage
	return onCommitDB, onAbortDB, updateValidators, updateValidators, nil
}

// InitiallyPrefersCommit returns true.
//
// Right now, *Commit (that is, remove the validator and reward them) is always
// preferred over *Abort (remove the validator but don't reward them.)
//
// TODO: A validator should receive a reward only if they are sufficiently
// responsive and correct during the time they are validating.
func (tx *UnsignedRewardValidatorTx) InitiallyPrefersCommit(*VM) bool {
	return tx.shouldPreferCommit
}

// RewardStakerTx creates a new transaction that proposes to remove the staker
// [validatorID] from the default validator set.
func (vm *VM) newRewardValidatorTx(txID ids.ID) (*Tx, error) {
	tx := &Tx{UnsignedTx: &UnsignedRewardValidatorTx{
		TxID: txID,
	}}
	return tx, tx.Sign(vm.codec, nil)
}
