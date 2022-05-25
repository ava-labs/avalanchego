// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var _ ProposalTx = &AddSubnetValidatorTx{}

type AddSubnetValidatorTx struct {
	*unsigned.AddSubnetValidatorTx

	txID        ids.ID // ID of signed add subnet validator tx
	signedBytes []byte // signed Tx bytes, needed to recreate signed.Tx
}

// Attempts to verify this transaction with the provided state.
func (tx *AddSubnetValidatorTx) SemanticVerify(
	verifier TxVerifier,
	parentState state.Mutable,
	creds []verify.Verifiable,
) error {
	clock := verifier.Clock()
	startTime := tx.StartTime()
	maxLocalStartTime := clock.Time().Add(MaxFutureStartTime)
	if startTime.After(maxLocalStartTime) {
		return ErrFutureStakeTime
	}

	_, _, err := tx.Execute(verifier, parentState, creds)
	// We ignore [errFutureStakeTime] here because an advanceTimeTx will be
	// issued before this transaction is issued.
	if errors.Is(err, ErrFutureStakeTime) {
		return nil
	}
	return err
}

// Execute this transaction.
func (tx *AddSubnetValidatorTx) Execute(
	verifier TxVerifier,
	parentState state.Mutable,
	creds []verify.Verifiable,
) (
	state.Versioned,
	state.Versioned,
	error,
) {
	ctx := verifier.Ctx()

	// Verify the tx is well-formed
	stx := &signed.Tx{
		Unsigned: tx.AddSubnetValidatorTx,
		Creds:    creds,
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
	case len(creds) == 0:
		return nil, nil, unsigned.ErrWrongNumberOfCredentials
	}

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	if verifier.Bootstrapped() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return nil, nil, fmt.Errorf(
				"validator's start time (%s) is at or after current chain timestamp (%s)",
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

		var vdrTx *unsigned.AddValidatorTx
		if err == nil {
			// This validator is attempting to validate with a currently
			// validing node.
			vdrTx, _ = currentValidator.AddValidatorTx()

			// Ensure that this transaction isn't a duplicate add validator tx.
			subnets := currentValidator.SubnetValidators()
			if _, validates := subnets[tx.Validator.Subnet]; validates {
				return nil, nil, fmt.Errorf(
					"already validating subnet %s",
					tx.Validator.Subnet,
				)
			}
		} else {
			// This validator is attempting to validate with a node that hasn't
			// started validating yet.
			vdrTx, _, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return nil, nil, unsigned.ErrDSValidatorSubset
				}
				return nil, nil, fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					tx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		if !tx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return nil, nil, unsigned.ErrDSValidatorSubset
		}

		// Ensure that this transaction isn't a duplicate add validator tx.
		pendingValidator := pendingStakers.GetValidator(tx.Validator.NodeID)
		subnets := pendingValidator.SubnetValidators()
		if _, validates := subnets[tx.Validator.Subnet]; validates {
			return nil, nil, fmt.Errorf(
				"already validating subnet %s",
				tx.Validator.Subnet,
			)
		}

		baseTxCredsLen := len(creds) - 1
		baseTxCreds := creds[:baseTxCredsLen]
		subnetCred := creds[baseTxCredsLen]

		subnetIntf, _, err := parentState.GetTx(tx.Validator.Subnet)
		if err != nil {
			if err == database.ErrNotFound {
				return nil, nil, unsigned.ErrDSValidatorSubset
			}
			return nil, nil, fmt.Errorf(
				"couldn't find subnet %s with %w",
				tx.Validator.Subnet,
				err,
			)
		}

		subnet, ok := subnetIntf.Unsigned.(*unsigned.CreateSubnetTx)
		if !ok {
			return nil, nil, fmt.Errorf(
				"%s is not a subnet",
				tx.Validator.Subnet,
			)
		}

		if err := verifier.FeatureExtension().VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
			return nil, nil, err
		}

		// Verify the flowcheck
		if err := verifier.SemanticVerifySpend(parentState,
			tx,
			tx.Ins,
			tx.Outs,
			baseTxCreds,
			verifier.PlatformConfig().TxFee,
			ctx.AVAXAssetID,
		); err != nil {
			return nil, nil, err
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
	utxos.ProduceOutputs(onAbortState, tx.txID, ctx.AVAXAssetID, tx.Outs)

	return onCommitState, onAbortState, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *AddSubnetValidatorTx) InitiallyPrefersCommit(verifier TxVerifier) bool {
	clock := verifier.Clock()
	return tx.StartTime().After(clock.Time())
}
