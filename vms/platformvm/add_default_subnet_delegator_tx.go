// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	safemath "github.com/ava-labs/gecko/utils/math"
)

var (
	errInvalidState  = errors.New("generated output isn't valid state")
	errInvalidAmount = errors.New("invalid amount")

	_ UnsignedProposalTx = &UnsignedAddDefaultSubnetDelegatorTx{}
	_ TimedTx            = &UnsignedAddDefaultSubnetDelegatorTx{}
)

// UnsignedAddDefaultSubnetDelegatorTx is an unsigned addDefaultSubnetDelegatorTx
type UnsignedAddDefaultSubnetDelegatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Describes the delegatee
	Validator DurationValidator `serialize:"true" json:"validator"`
	// Where to send staked tokens when done validating
	Stake []*avax.TransferableOutput `serialize:"true" json:"stake"`
	// Where to send staking rewards when done validating
	RewardsOwner verify.Verifiable `serialize:"true" json:"rewardsOwner"`
}

// StartTime of this validator
func (tx *UnsignedAddDefaultSubnetDelegatorTx) StartTime() time.Time {
	return tx.Validator.StartTime()
}

// EndTime of this validator
func (tx *UnsignedAddDefaultSubnetDelegatorTx) EndTime() time.Time {
	return tx.Validator.EndTime()
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddDefaultSubnetDelegatorTx) Verify(
	ctx *snow.Context,
	c codec.Codec,
	feeAmount uint64,
	feeAssetID ids.ID,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.Verify(ctx, c); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.RewardsOwner); err != nil {
		return err
	}

	totalStakeWeight := uint64(0)
	for _, out := range tx.Stake {
		if err := out.Verify(); err != nil {
			return err
		}
		newWeight, err := safemath.Add64(totalStakeWeight, out.Output().Amount())
		if err != nil {
			return err
		}
		totalStakeWeight = newWeight
	}

	switch {
	case !avax.IsSortedTransferableOutputs(tx.Stake, c):
		return errOutputsNotSorted
	case totalStakeWeight != tx.Validator.Wght:
		return errInvalidAmount
	case tx.Validator.Wght < MinimumStakeAmount:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	}

	// verify the flow check
	if err := syntacticVerifySpend(
		tx.Ins,
		tx.Outs,
		tx.Stake,
		tx.Validator.Wght,
		feeAmount,
		feeAssetID,
	); err != nil {
		return err
	}

	// cache that this is valid
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAddDefaultSubnetDelegatorTx) SemanticVerify(
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
	// Verify the tx is well-formed
	if err := tx.Verify(vm.Ctx, vm.codec, vm.txFee, vm.avaxAssetID); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Ensure the proposed validator starts after the current timestamp
	if currentTimestamp, err := vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("chain timestamp (%s) not before validator's start time (%s)",
			currentTimestamp,
			validatorStartTime)}
	}

	// Ensure that the period this delegator is running is a subset of the time
	// the validator is running. First, see if the validator is currently
	// running.
	currentValidators, err := vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("couldn't get current validators of default subnet: %w", err)}
	}
	pendingValidators, err := vm.getPendingValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get pending validators of default subnet: %w", err)}
	}

	if validator, err := currentValidators.getDefaultSubnetStaker(tx.Validator.NodeID); err == nil {
		unsignedValidator := validator.UnsignedTx.(*UnsignedAddDefaultSubnetValidatorTx)
		if !tx.Validator.BoundedBy(unsignedValidator.StartTime(), unsignedValidator.EndTime()) {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
	} else {
		// They aren't currently validating, so check to see if they will
		// validate in the future.
		validator, err := pendingValidators.getDefaultSubnetStaker(tx.Validator.NodeID)
		if err != nil {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
		unsignedValidator := validator.UnsignedTx.(*UnsignedAddDefaultSubnetValidatorTx)
		if !tx.Validator.BoundedBy(unsignedValidator.StartTime(), unsignedValidator.EndTime()) {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(db, tx, tx.Ins, outs, stx.Creds); err != nil {
		return nil, nil, nil, nil, err
	}

	txID := tx.ID()

	// Set up the DB if this tx is committed
	onCommitDB := versiondb.New(db)
	// Consume the UTXOS
	if err := vm.consumeInputs(onCommitDB, tx.Ins); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	// Produce the UTXOS
	if err := vm.produceOutputs(onCommitDB, txID, tx.Outs); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// Add the delegator to the pending validators heap
	pendingValidators.Add(stx)
	// If this proposal is committed, update the pending validator set to include the delegator
	if err := vm.putPendingValidators(onCommitDB, pendingValidators, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// Set up the DB if this tx is aborted
	onAbortDB := versiondb.New(db)
	// Consume the UTXOS
	if err := vm.consumeInputs(onAbortDB, tx.Ins); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	// Produce the UTXOS
	if err := vm.produceOutputs(onAbortDB, txID, outs); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	return onCommitDB, onAbortDB, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *UnsignedAddDefaultSubnetDelegatorTx) InitiallyPrefersCommit(vm *VM) bool {
	return tx.StartTime().After(vm.clock.Time())
}

// Creates a new transaction
func (vm *VM) newAddDefaultSubnetDelegatorTx(
	stakeAmt, // Amount the delegator stakes
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they stop delegating
	nodeID ids.ShortID, // ID of the node we are delegating to
	rewardAddress ids.ShortID, // Address to returned staked tokens (and maybe reward) to
	keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens + fee
) (*Tx, error) {
	ins, unlockedOuts, lockedOuts, signers, err := vm.stake(vm.DB, keys, stakeAmt, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// Create the tx
	utx := &UnsignedAddDefaultSubnetDelegatorTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Ins:          ins,
			Outs:         unlockedOuts,
		}},
		Validator: DurationValidator{
			Validator: Validator{
				NodeID: nodeID,
				Wght:   stakeAmt,
			},
			Start: startTime,
			End:   endTime,
		},
		Stake: lockedOuts,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.Ctx, vm.codec, vm.txFee, vm.avaxAssetID)
}
