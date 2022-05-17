// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	pChainValidator "github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

var (
	_ StatefulProposalTx = &StatefulAddValidatorTx{}

	errNilTx                     = errors.New("tx is nil")
	errWeightTooSmall            = errors.New("weight of this validator is too low")
	errWeightTooLarge            = errors.New("weight of this validator is too large")
	errStakeTooShort             = errors.New("staking period is too short")
	errStakeTooLong              = errors.New("staking period is too long")
	errInsufficientDelegationFee = errors.New("staker charges an insufficient delegation fee")
	errFutureStakeTime           = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", maxFutureStartTime)
)

// StatefulAddValidatorTx is an unsigned addValidatorTx
type StatefulAddValidatorTx struct {
	*unsigned.AddValidatorTx `serialize:"true"`
}

// Attempts to verify this transaction with the provided state.
func (tx *StatefulAddValidatorTx) SemanticVerify(vm *VM, parentState MutableState, stx *signed.Tx) error {
	startTime := tx.StartTime()
	maxLocalStartTime := vm.clock.Time().Add(maxFutureStartTime)
	if startTime.After(maxLocalStartTime) {
		return errFutureStakeTime
	}

	_, _, err := tx.Execute(vm, parentState, stx)
	// We ignore [errFutureStakeTime] here because an advanceTimeTx will be
	// issued before this transaction is issued.
	if errors.Is(err, errFutureStakeTime) {
		return nil
	}
	return err
}

// Execute this transaction.
func (tx *StatefulAddValidatorTx) Execute(
	vm *VM,
	parentState MutableState,
	stx *signed.Tx,
) (
	VersionedState,
	VersionedState,
	error,
) {
	// Verify the tx is well-formed
	if err := tx.SyntacticVerify(vm.ctx); err != nil {
		return nil, nil, err
	}

	switch {
	case tx.Validator.Wght < vm.MinValidatorStake: // Ensure validator is staking at least the minimum amount
		return nil, nil, errWeightTooSmall
	case tx.Validator.Wght > vm.MaxValidatorStake: // Ensure validator isn't staking too much
		return nil, nil, errWeightTooLarge
	case tx.Shares < vm.MinDelegationFee:
		return nil, nil, errInsufficientDelegationFee
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < vm.MinStakeDuration: // Ensure staking length is not too short
		return nil, nil, errStakeTooShort
	case duration > vm.MaxStakeDuration: // Ensure staking length is not too long
		return nil, nil, errStakeTooLong
	}

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if vm.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		startTime := tx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return nil, nil, fmt.Errorf(
				"validator's start time (%s) at or before current timestamp (%s)",
				startTime,
				currentTimestamp,
			)
		}

		// Ensure this validator isn't currently a validator.
		_, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err == nil {
			return nil, nil, fmt.Errorf(
				"%s is already a primary network validator",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Ensure this validator isn't about to become a validator.
		_, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
		if err == nil {
			return nil, nil, fmt.Errorf(
				"%s is about to become a primary network validator",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is about to become a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Verify the flowcheck
		if err := vm.semanticVerifySpend(
			parentState,
			tx.AddValidatorTx,
			tx.Ins,
			outs,
			stx.Creds,
			vm.AddStakerTxFee,
			vm.ctx.AVAXAssetID,
		); err != nil {
			return nil, nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(maxFutureStartTime)
		if startTime.After(maxStartTime) {
			return nil, nil, errFutureStakeTime
		}
	}

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := newVersionedState(parentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	consumeInputs(onCommitState, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	produceOutputs(onCommitState, txID, vm.ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := newVersionedState(parentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	consumeInputs(onAbortState, tx.Ins)
	// Produce the UTXOS
	produceOutputs(onAbortState, txID, vm.ctx.AVAXAssetID, outs)

	return onCommitState, onAbortState, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *StatefulAddValidatorTx) InitiallyPrefersCommit(vm *VM) bool {
	return tx.StartTime().After(vm.clock.Time())
}

// NewAddValidatorTx returns a new NewAddValidatorTx
func (vm *VM) newAddValidatorTx(
	stakeAmt, // Amount the validator stakes
	startTime, // Unix time they start validating
	endTime uint64, // Unix time they stop validating
	nodeID ids.NodeID, // ID of the node we want to validate with
	rewardAddress ids.ShortID, // Address to send reward to, if applicable
	shares uint32, // 10,000 times percentage of reward taken from delegators
	keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	ins, unlockedOuts, lockedOuts, signers, err := vm.stake(keys, stakeAmt, vm.AddStakerTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// Create the tx
	utx := &unsigned.AddValidatorTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         unlockedOuts,
		}},
		Validator: pChainValidator.Validator{
			NodeID: nodeID,
			Start:  startTime,
			End:    endTime,
			Wght:   stakeAmt,
		},
		Stake: lockedOuts,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		Shares: shares,
	}
	tx := &signed.Tx{Unsigned: utx}
	if err := tx.Sign(Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.SyntacticVerify(vm.ctx)
}
