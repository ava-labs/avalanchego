// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"

	txstate "github.com/ava-labs/avalanchego/vms/platformvm/state/transactions"
	p_validator "github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

var (
	_ ProposalTx = &AddDelegatorTx{}

	ErrInvalidState  = errors.New("generated output isn't valid state")
	ErrOverDelegated = errors.New("validator would be over delegated")
)

// maxValidatorWeightFactor is the maximum factor of the validator stake
// that is allowed to be placed on a validator.
const maxValidatorWeightFactor uint64 = 5

type AddDelegatorTx struct {
	*unsigned.AddDelegatorTx

	txID        ids.ID // ID of signed add subnet validator tx
	signedBytes []byte // signed Tx bytes, needed to recreate signed.Tx
	creds       []verify.Verifiable
}

// Attempts to verify this transaction with the provided state.
func (tx *AddDelegatorTx) SemanticVerify(
	verifier TxVerifier,
	parentState state.Mutable,
) error {
	clock := verifier.Clock()
	startTime := tx.StartTime()
	maxLocalStartTime := clock.Time().Add(MaxFutureStartTime)
	if startTime.After(maxLocalStartTime) {
		return ErrFutureStakeTime
	}

	_, _, err := tx.Execute(verifier, parentState)
	// We ignore [errFutureStakeTime] here because an advanceTimeTx will be
	// issued before this transaction is issued.
	if errors.Is(err, ErrFutureStakeTime) {
		return nil
	}
	return err
}

// Execute this transaction.
func (tx *AddDelegatorTx) Execute(
	verifier TxVerifier,
	parentState state.Mutable,
) (
	state.Versioned,
	state.Versioned,
	error,
) {
	ctx := verifier.Ctx()

	// Verify the tx is well-formed
	stx := &signed.Tx{
		Unsigned: tx.AddDelegatorTx,
		Creds:    tx.creds,
	}
	stx.Initialize(tx.UnsignedBytes(), tx.signedBytes)
	if err := stx.SyntacticVerify(verifier.Ctx()); err != nil {
		return nil, nil, err
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < verifier.PlatformConfig().MinStakeDuration: // Ensure staking length is not too short
		return nil, nil, ErrStakeTooShort
	case duration > verifier.PlatformConfig().MaxStakeDuration: // Ensure staking length is not too long
		return nil, nil, ErrStakeTooLong
	case tx.Validator.Wght < verifier.PlatformConfig().MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, nil, p_validator.ErrWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	if verifier.Bootstrapped() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return nil, nil, fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		currentValidator, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		pendingValidator := pendingStakers.GetValidator(tx.Validator.NodeID)
		pendingDelegators := pendingValidator.Delegators()

		var (
			vdrTx                  *unsigned.AddValidatorTx
			currentDelegatorWeight uint64
			currentDelegators      []signed.DelegatorAndID
		)
		if err == nil {
			// This delegator is attempting to delegate to a currently validing
			// node.
			vdrTx, _ = currentValidator.AddValidatorTx()
			currentDelegatorWeight = currentValidator.DelegatorWeight()
			currentDelegators = currentValidator.Delegators()
		} else {
			// This delegator is attempting to delegate to a node that hasn't
			// started validating yet.
			vdrTx, _, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return nil, nil, unsigned.ErrDelegatorSubset
				}
				return nil, nil, fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					tx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this delegator delegates is a subset of the
		// time the validator validates.
		if !tx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return nil, nil, unsigned.ErrDelegatorSubset
		}

		// Ensure that the period this delegator delegates wouldn't become over
		// delegated.
		vdrWeight := vdrTx.Weight()
		currentWeight, err := math.Add64(vdrWeight, currentDelegatorWeight)
		if err != nil {
			return nil, nil, err
		}

		maximumWeight, err := math.Mul64(maxValidatorWeightFactor, vdrWeight)
		if err != nil {
			return nil, nil, api.ErrStakeOverflow
		}

		if !currentTimestamp.Before(verifier.PlatformConfig().ApricotPhase3Time) {
			maximumWeight = math.Min64(maximumWeight, verifier.PlatformConfig().MaxValidatorStake)
		}

		canDelegate, err := txstate.CanDelegate(
			currentDelegators,
			pendingDelegators,
			tx.AddDelegatorTx,
			currentWeight,
			maximumWeight,
		)
		if err != nil {
			return nil, nil, err
		}
		if !canDelegate {
			return nil, nil, ErrOverDelegated
		}

		// Verify the flowcheck
		if err := verifier.SemanticVerifySpend(
			parentState,
			tx,
			tx.Ins,
			outs,
			tx.creds,
			verifier.PlatformConfig().AddStakerTxFee,
			ctx.AVAXAssetID,
		); err != nil {
			return nil, nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if validatorStartTime.After(maxStartTime) {
			return nil, nil, ErrFutureStakeTime
		}
	}

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := state.NewVersioned(parentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	utxos.ConsumeInputs(onCommitState, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(onCommitState, tx.txID, ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	utxos.ConsumeInputs(onAbortState, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(onAbortState, tx.txID, ctx.AVAXAssetID, outs)

	return onCommitState, onAbortState, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *AddDelegatorTx) InitiallyPrefersCommit(verifier TxVerifier) bool {
	clock := verifier.Clock()
	return tx.StartTime().After(clock.Time())
}
