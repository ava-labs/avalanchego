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
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	safemath "github.com/ava-labs/gecko/utils/math"
)

var (
	errInvalidState = errors.New("generated output isn't valid state")
)

// UnsignedAddDefaultSubnetDelegatorTx is an unsigned addDefaultSubnetDelegatorTx
type UnsignedAddDefaultSubnetDelegatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Describes the delegatee
	DurationValidator `serialize:"true"`
	// Where to send staked tokens when done validating
	Stake []*ava.TransferableOutput `serialize:"true" json:"stake"`
	// Where to send staking rewards when done validating
	RewardsOwner verify.Verifiable `serialize:"true" json:"rewardsOwner"`
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *UnsignedAddDefaultSubnetDelegatorTx) initialize(vm *VM, bytes []byte) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAddDefaultSubnetDelegatorTx: %w", err)
	}
	return nil
}

var (
	errInvalidAmount = errors.New("invalid amount")
)

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddDefaultSubnetDelegatorTx) Verify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := verify.All(
		&tx.BaseTx,
		&tx.DurationValidator,
		tx.RewardsOwner,
	); err != nil {
		return err
	}

	returnedWeight := uint64(0)
	for _, out := range tx.Stake {
		if err := out.Verify(); err != nil {
			return err
		}
		newWeight, err := safemath.Add64(returnedWeight, out.Output().Amount())
		if err != nil {
			return err
		}
		returnedWeight = newWeight
	}

	switch {
	case !ava.IsSortedTransferableOutputs(tx.Stake, Codec):
		return errOutputsNotSorted
	case returnedWeight != tx.Wght:
		return errInvalidAmount
	case tx.Wght < MinimumStakeAmount:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	}

	// verify the flow check
	if err := syntacticVerifySpend(tx.Ins, tx.Outs, tx.Stake, tx.Wght, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}

	// cache that this is valid
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAddDefaultSubnetDelegatorTx) SemanticVerify(
	db database.Database,
	stx *ProposalTx,
) (
	*versiondb.Database,
	*versiondb.Database,
	func() error,
	func() error,
	TxError,
) {
	// Verify the tx is well-formed
	if err := tx.Verify(); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	// Ensure the proposed validator starts after the current timestamp
	if currentTimestamp, err := tx.vm.getTimestamp(db); err != nil {
		return nil, nil, nil, nil, tempError{err}
	} else if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
		return nil, nil, nil, nil, permError{fmt.Errorf("chain timestamp (%s) not before validator's start time (%s)",
			currentTimestamp,
			validatorStartTime)}
	}

	// Ensure that the period this delegator is running is a subset of the time
	// the validator is running. First, see if the validator is currently
	// running.
	currentValidators, err := tx.vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, permError{fmt.Errorf("couldn't get current validators of default subnet: %w", err)}
	}
	pendingValidators, err := tx.vm.getPendingValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, nil, nil, nil, tempError{fmt.Errorf("couldn't get pending validators of default subnet: %w", err)}
	}

	if validator, err := currentValidators.getDefaultSubnetStaker(tx.NodeID); err == nil {
		unsignedValidator := validator.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx)
		if !tx.DurationValidator.BoundedBy(unsignedValidator.StartTime(), unsignedValidator.EndTime()) {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
	} else {
		// They aren't currently validating, so check to see if they will
		// validate in the future.
		validator, err := pendingValidators.getDefaultSubnetStaker(tx.NodeID)
		if err != nil {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
		unsignedValidator := validator.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx)
		if !tx.DurationValidator.BoundedBy(unsignedValidator.StartTime(), unsignedValidator.EndTime()) {
			return nil, nil, nil, nil, permError{errDSValidatorSubset}
		}
	}

	outs := make([]*ava.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	// Verify the flowcheck
	if err := tx.vm.semanticVerifySpend(db, tx, tx.Ins, outs, stx.Credentials); err != nil {
		return nil, nil, nil, nil, err
	}

	txID := tx.ID()

	// Set up the DB if this tx is committed
	onCommitDB := versiondb.New(db)
	// Consume the UTXOS
	if err := tx.vm.consumeInputs(onCommitDB, tx.Ins); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	// Produce the UTXOS
	if err := tx.vm.produceOutputs(onCommitDB, txID, tx.Outs); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// Add the delegator to the pending validators heap
	pendingValidators.Add(stx)
	// If this proposal is committed, update the pending validator set to include the delegator
	if err := tx.vm.putPendingValidators(onCommitDB, pendingValidators, constants.DefaultSubnetID); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	// Set up the DB if this tx is aborted
	onAbortDB := versiondb.New(db)
	// Consume the UTXOS
	if err := tx.vm.consumeInputs(onAbortDB, tx.Ins); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}
	// Produce the UTXOS
	if err := tx.vm.produceOutputs(onAbortDB, txID, outs); err != nil {
		return nil, nil, nil, nil, tempError{err}
	}

	return onCommitDB, onAbortDB, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *UnsignedAddDefaultSubnetDelegatorTx) InitiallyPrefersCommit() bool {
	return tx.StartTime().After(tx.vm.clock.Time())
}

// Creates a new transaction
func (vm *VM) newAddDefaultSubnetDelegatorTx(
	stakeAmt, // Amount the delegator stakes
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they stop delegating
	nodeID ids.ShortID, // ID of the node we are delegating to
	rewardAddress ids.ShortID, // Address to returned staked tokens (and maybe reward) to
	keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens + fee
) (*ProposalTx, error) {
	ins, unlockedOuts, lockedOuts, signers, err := vm.spend(vm.DB, keys, stakeAmt, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// Create the tx
	utx := &UnsignedAddDefaultSubnetDelegatorTx{
		BaseTx: BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Ins:          ins,
			Outs:         unlockedOuts,
		},
		DurationValidator: DurationValidator{
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
	tx := &ProposalTx{UnsignedProposalTx: utx}
	if err := vm.signProposalTx(tx, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify()
}
