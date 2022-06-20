// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/transactions"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var (
	_ txs.Visitor = &ProposalTxExecutor{}

	errWeightTooSmall            = errors.New("weight of this validator is too low")
	errWeightTooLarge            = errors.New("weight of this validator is too large")
	errStakeTooShort             = errors.New("staking period is too short")
	errStakeTooLong              = errors.New("staking period is too long")
	errInsufficientDelegationFee = errors.New("staker charges an insufficient delegation fee")
	errFutureStakeTime           = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	errWrongNumberOfCredentials  = errors.New("should have the same number of credentials as inputs")
	errValidatorSubset           = errors.New("all subnets' staking period must be a subset of the primary network")
	errStakeOverflow             = errors.New("validator stake exceeds limit")
	errInvalidState              = errors.New("generated output isn't valid state")
	errOverDelegated             = errors.New("validator would be over delegated")
	errShouldBeDSValidator       = errors.New("expected validator to be in the primary network")
	errWrongTxType               = errors.New("wrong transaction type")
	errInvalidID                 = errors.New("invalid ID")
)

const (
	// Maximum future start time for staking/delegating
	MaxFutureStartTime = 24 * 7 * 2 * time.Hour

	// SyncBound is the synchrony bound used for safe decision making
	SyncBound = 10 * time.Second

	MaxValidatorWeightFactor = uint64(5)
)

type ProposalTxExecutor struct {
	// inputs
	*Backend
	ParentState state.Mutable
	Tx          *txs.Tx

	// outputs
	OnCommit      state.Versioned
	OnAbort       state.Versioned
	PrefersCommit bool
}

func (*ProposalTxExecutor) CreateChainTx(*txs.CreateChainTx) error   { return errWrongTxType }
func (*ProposalTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error { return errWrongTxType }
func (*ProposalTxExecutor) ImportTx(*txs.ImportTx) error             { return errWrongTxType }
func (*ProposalTxExecutor) ExportTx(*txs.ExportTx) error             { return errWrongTxType }

func (e *ProposalTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	// Verify the tx is well-formed
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	switch {
	case tx.Validator.Wght < e.Cfg.MinValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall

	case tx.Validator.Wght > e.Cfg.MaxValidatorStake:
		// Ensure validator isn't staking too much
		return errWeightTooLarge

	case tx.Shares < e.Cfg.MinDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return errInsufficientDelegationFee
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < e.Cfg.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > e.Cfg.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong
	}

	currentStakers := e.ParentState.CurrentStakerChainState()
	pendingStakers := e.ParentState.PendingStakerChainState()

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if e.Bootstrapped.GetValue() {
		currentTimestamp := e.ParentState.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		startTime := tx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return fmt.Errorf(
				"validator's start time (%s) at or before current timestamp (%s)",
				startTime,
				currentTimestamp,
			)
		}

		// Ensure this validator isn't currently a validator.
		_, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err == nil {
			return fmt.Errorf(
				"%s is already a primary network validator",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Ensure this validator isn't about to become a validator.
		_, _, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
		if err == nil {
			return fmt.Errorf(
				"%s is about to become a primary network validator",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is about to become a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Verify the flowcheck
		if err := e.SpendHandler.SemanticVerifySpend(
			e.ParentState,
			tx,
			tx.Ins,
			outs,
			e.Tx.Creds,
			e.Cfg.AddStakerTxFee,
			e.Ctx.AVAXAssetID,
		); err != nil {
			return fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if startTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(e.Tx)
	e.OnCommit = state.NewVersioned(e.ParentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	utxos.ConsumeInputs(e.OnCommit, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(e.OnCommit, txID, e.Ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	e.OnAbort = state.NewVersioned(e.ParentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	utxos.ConsumeInputs(e.OnAbort, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(e.OnAbort, txID, e.Ctx.AVAXAssetID, outs)

	e.PrefersCommit = tx.StartTime().After(e.Clk.Time())
	return nil
}

func (e *ProposalTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	// Verify the tx is well-formed
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < e.Cfg.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > e.Cfg.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong

	case len(e.Tx.Creds) == 0:
		// Ensure there is at least one credential for the subnet authorization
		return errWrongNumberOfCredentials
	}

	currentStakers := e.ParentState.CurrentStakerChainState()
	pendingStakers := e.ParentState.PendingStakerChainState()

	if e.Bootstrapped.GetValue() {
		currentTimestamp := e.ParentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"validator's start time (%s) is at or after current chain timestamp (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		currentValidator, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		var vdrTx *txs.AddValidatorTx
		if err == nil {
			// This validator is attempting to validate with a currently
			// validing node.
			vdrTx, _ = currentValidator.AddValidatorTx()

			// Ensure that this transaction isn't a duplicate add validator tx.
			subnets := currentValidator.SubnetValidators()
			if _, validates := subnets[tx.Validator.Subnet]; validates {
				return fmt.Errorf(
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
					return errValidatorSubset
				}
				return fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					tx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		if !tx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return errValidatorSubset
		}

		// Ensure that this transaction isn't a duplicate add validator tx.
		pendingValidator := pendingStakers.GetValidator(tx.Validator.NodeID)
		subnets := pendingValidator.SubnetValidators()
		if _, validates := subnets[tx.Validator.Subnet]; validates {
			return fmt.Errorf(
				"already validating subnet %s",
				tx.Validator.Subnet,
			)
		}

		baseTxCredsLen := len(e.Tx.Creds) - 1
		baseTxCreds := e.Tx.Creds[:baseTxCredsLen]
		subnetCred := e.Tx.Creds[baseTxCredsLen]

		subnetIntf, _, err := e.ParentState.GetTx(tx.Validator.Subnet)
		if err != nil {
			if err == database.ErrNotFound {
				return errValidatorSubset
			}
			return fmt.Errorf(
				"couldn't find subnet %s with %w",
				tx.Validator.Subnet,
				err,
			)
		}

		subnet, ok := subnetIntf.Unsigned.(*txs.CreateSubnetTx)
		if !ok {
			return fmt.Errorf(
				"%s is not a subnet",
				tx.Validator.Subnet,
			)
		}

		if err := e.Fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
			return err
		}

		// Verify the flowcheck
		if err := e.SpendHandler.SemanticVerifySpend(
			e.ParentState,
			tx,
			tx.Ins,
			tx.Outs,
			baseTxCreds,
			e.Cfg.TxFee,
			e.Ctx.AVAXAssetID,
		); err != nil {
			return err
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if validatorStartTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(e.Tx)
	e.OnCommit = state.NewVersioned(e.ParentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	utxos.ConsumeInputs(e.OnCommit, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(e.OnCommit, txID, e.Ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	e.OnAbort = state.NewVersioned(e.ParentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	utxos.ConsumeInputs(e.OnAbort, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(e.OnAbort, txID, e.Ctx.AVAXAssetID, tx.Outs)

	e.PrefersCommit = tx.StartTime().After(e.Clk.Time())
	return nil
}

func (e *ProposalTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	// Verify the tx is well-formed
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < e.Cfg.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > e.Cfg.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong

	case tx.Validator.Wght < e.Cfg.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	currentStakers := e.ParentState.CurrentStakerChainState()
	pendingStakers := e.ParentState.PendingStakerChainState()

	if e.Bootstrapped.GetValue() {
		currentTimestamp := e.ParentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		currentValidator, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		pendingValidator := pendingStakers.GetValidator(tx.Validator.NodeID)
		pendingDelegators := pendingValidator.Delegators()

		var (
			vdrTx                  *txs.AddValidatorTx
			currentDelegatorWeight uint64
			currentDelegators      []transactions.DelegatorAndID
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
					return transactions.ErrDelegatorSubset
				}
				return fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					tx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this delegator delegates is a subset of the
		// time the validator validates.
		if !tx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return transactions.ErrDelegatorSubset
		}

		// Ensure that the period this delegator delegates wouldn't become over
		// delegated.
		vdrWeight := vdrTx.Weight()
		currentWeight, err := math.Add64(vdrWeight, currentDelegatorWeight)
		if err != nil {
			return err
		}

		maximumWeight, err := math.Mul64(MaxValidatorWeightFactor, vdrWeight)
		if err != nil {
			return errStakeOverflow
		}

		if !currentTimestamp.Before(e.Cfg.ApricotPhase3Time) {
			maximumWeight = math.Min64(maximumWeight, e.Cfg.MaxValidatorStake)
		}

		canDelegate, err := transactions.CanDelegate(
			currentDelegators,
			pendingDelegators,
			tx,
			currentWeight,
			maximumWeight,
		)
		if err != nil {
			return err
		}
		if !canDelegate {
			return errOverDelegated
		}

		// Verify the flowcheck
		if err := e.SpendHandler.SemanticVerifySpend(
			e.ParentState,
			tx,
			tx.Ins,
			outs,
			e.Tx.Creds,
			e.Cfg.AddStakerTxFee,
			e.Ctx.AVAXAssetID,
		); err != nil {
			return fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if validatorStartTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(e.Tx)
	e.OnCommit = state.NewVersioned(e.ParentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	utxos.ConsumeInputs(e.OnCommit, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(e.OnCommit, txID, e.Ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	e.OnAbort = state.NewVersioned(e.ParentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	utxos.ConsumeInputs(e.OnAbort, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(e.OnAbort, txID, e.Ctx.AVAXAssetID, outs)

	e.PrefersCommit = tx.StartTime().After(e.Clk.Time())
	return nil
}

func (e *ProposalTxExecutor) AdvanceTimeTx(tx *txs.AdvanceTimeTx) error {
	switch {
	case tx == nil:
		return txs.ErrNilTx
	case len(e.Tx.Creds) != 0:
		return errWrongNumberOfCredentials
	}

	// Validate txTimestamp by comparing it with relevant time quantities
	txTimestamp := tx.Timestamp()
	localTimestamp := e.Clk.Time()
	chainTimestamp := e.ParentState.GetTimestamp()
	nextStakerChangeTime, err := e.ParentState.GetNextStakerChangeTime()
	if err != nil {
		return err
	}

	if err := ValidateProposedChainTime(
		txTimestamp,
		chainTimestamp,
		nextStakerChangeTime,
		localTimestamp,
	); err != nil {
		return err
	}

	// update State if this proposal is committed
	CurrentStakerStates := e.ParentState.CurrentStakerChainState()
	PendingStakerStates := e.ParentState.PendingStakerChainState()
	currentSupply := e.ParentState.GetCurrentSupply()

	newlyCurrentStakerStates, newlyPendingStakerStates, updatedSupply, err := UpdateStakerSet(CurrentStakerStates, PendingStakerStates, currentSupply, e.Backend, txTimestamp)
	if err != nil {
		return err
	}

	e.OnCommit = state.NewVersioned(e.ParentState, newlyCurrentStakerStates, newlyPendingStakerStates)
	e.OnCommit.SetTimestamp(txTimestamp)
	e.OnCommit.SetCurrentSupply(updatedSupply)

	// State doesn't change if this proposal is aborted
	e.OnAbort = state.NewVersioned(e.ParentState, CurrentStakerStates, PendingStakerStates)
	e.PrefersCommit = !txTimestamp.After(localTimestamp.Add(SyncBound))
	return nil
}

func UpdateStakerSet(
	currentStakerStates transactions.CurrentStakerState,
	pendingStakerStates transactions.PendingStakerState,
	currentSupply uint64,
	backend *Backend,
	proposedChainTime time.Time,
) (
	transactions.CurrentStakerState,
	transactions.PendingStakerState,
	uint64, // updatedSupply
	error,
) {
	toAddValidatorsWithRewardToCurrent := []*transactions.ValidatorReward(nil)
	toAddDelegatorsWithRewardToCurrent := []*transactions.ValidatorReward(nil)
	toAddWithoutRewardToCurrent := []*txs.Tx(nil)
	numToRemoveFromPending := 0
	var err error

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp. [PendingStakerStates.Stakers()] is sorted in order
	// of increasing startTime
PendingStakerStateLoop:
	for _, stakerTx := range pendingStakerStates.Stakers() {
		switch staker := stakerTx.Unsigned.(type) {
		case *txs.AddDelegatorTx:
			if staker.StartTime().After(proposedChainTime) {
				break PendingStakerStateLoop
			}

			r := backend.Rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = math.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, 0, err
			}

			toAddDelegatorsWithRewardToCurrent = append(toAddDelegatorsWithRewardToCurrent, &transactions.ValidatorReward{
				AddStakerTx:     stakerTx,
				PotentialReward: r,
			})
			numToRemoveFromPending++
		case *txs.AddValidatorTx:
			if staker.StartTime().After(proposedChainTime) {
				break PendingStakerStateLoop
			}

			r := backend.Rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = math.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, 0, err
			}

			toAddValidatorsWithRewardToCurrent = append(toAddValidatorsWithRewardToCurrent, &transactions.ValidatorReward{
				AddStakerTx:     stakerTx,
				PotentialReward: r,
			})
			numToRemoveFromPending++
		case *txs.AddSubnetValidatorTx:
			if staker.StartTime().After(proposedChainTime) {
				break PendingStakerStateLoop
			}

			// If this staker should already be removed, then we should just
			// never add them.
			if staker.EndTime().After(proposedChainTime) {
				toAddWithoutRewardToCurrent = append(toAddWithoutRewardToCurrent, stakerTx)
			}
			numToRemoveFromPending++
		default:
			return nil, nil, 0, fmt.Errorf("expected validator but got %T", stakerTx.Unsigned)
		}
	}
	newlyPendingStakerStates := pendingStakerStates.DeleteStakers(numToRemoveFromPending)

	// Remove from the staker set any subnet validators whose endTime is at or
	// before the new timestamp
	numToRemoveFromCurrent := 0
CurrentStakerStateLoop:
	for _, tx := range currentStakerStates.Stakers() {
		switch staker := tx.Unsigned.(type) {
		case *txs.AddSubnetValidatorTx:
			if staker.EndTime().After(proposedChainTime) {
				break CurrentStakerStateLoop
			}

			numToRemoveFromCurrent++
		case *txs.AddValidatorTx, *txs.AddDelegatorTx:
			// We shouldn't be removing any primary network validators here
			break CurrentStakerStateLoop
		default:
			return nil,
				nil,
				0,
				fmt.Errorf("expected tx type *txs.AddValidatorTx or *txs.AddDelegatorTx but got %T", staker)
		}
	}
	newlyCurrentStakerStates, err := currentStakerStates.UpdateStakers(
		toAddValidatorsWithRewardToCurrent,
		toAddDelegatorsWithRewardToCurrent,
		toAddWithoutRewardToCurrent,
		numToRemoveFromCurrent,
	)
	if err != nil {
		return nil, nil, 0, err
	}

	return newlyCurrentStakerStates, newlyPendingStakerStates, currentSupply, nil
}

func (e *ProposalTxExecutor) RewardValidatorTx(tx *txs.RewardValidatorTx) error {
	switch {
	case tx == nil:
		return txs.ErrNilTx
	case tx.TxID == ids.Empty:
		return errInvalidID
	case len(e.Tx.Creds) != 0:
		return errWrongNumberOfCredentials
	}

	currentStakers := e.ParentState.CurrentStakerChainState()
	stakerTx, stakerReward, err := currentStakers.GetNextStaker()
	if err == database.ErrNotFound {
		return fmt.Errorf("failed to get next staker stop time: %w", err)
	}
	if err != nil {
		return err
	}

	stakerID := stakerTx.ID()
	if stakerID != tx.TxID {
		return fmt.Errorf(
			"attempting to remove TxID: %s. Should be removing %s",
			tx.TxID,
			stakerID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentTime := e.ParentState.GetTimestamp()
	staker, ok := stakerTx.Unsigned.(txs.StakerTx)
	if !ok {
		return errWrongTxType
	}
	if endTime := staker.EndTime(); !endTime.Equal(currentTime) {
		return fmt.Errorf(
			"attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			endTime,
		)
	}

	newlyCurrentStakers, err := currentStakers.DeleteNextStaker()
	if err != nil {
		return err
	}

	pendingStakers := e.ParentState.PendingStakerChainState()
	e.OnCommit = state.NewVersioned(e.ParentState, newlyCurrentStakers, pendingStakers)
	e.OnAbort = state.NewVersioned(e.ParentState, newlyCurrentStakers, pendingStakers)

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply := e.OnAbort.GetCurrentSupply()
	newSupply, err := math.Sub64(currentSupply, stakerReward)
	if err != nil {
		return err
	}
	e.OnAbort.SetCurrentSupply(newSupply)

	var (
		nodeID    ids.NodeID
		startTime time.Time
	)
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case *txs.AddValidatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: e.Ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			e.OnCommit.AddUTXO(utxo)
			e.OnAbort.AddUTXO(utxo)
		}

		// Provide the reward here
		if stakerReward > 0 {
			outIntf, err := e.Fx.CreateOutput(stakerReward, uStakerTx.RewardsOwner)
			if err != nil {
				return fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return errInvalidState
			}

			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: e.Ctx.AVAXAssetID},
				Out:   out,
			}

			e.OnCommit.AddUTXO(utxo)
			e.OnCommit.AddRewardUTXO(tx.TxID, utxo)
		}

		// Handle reward preferences
		nodeID = uStakerTx.Validator.ID()
		startTime = uStakerTx.StartTime()
	case *txs.AddDelegatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: e.Ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			e.OnCommit.AddUTXO(utxo)
			e.OnAbort.AddUTXO(utxo)
		}

		// We're removing a delegator, so we need to fetch the validator they
		// are delgated to.
		vdr, err := currentStakers.GetValidator(uStakerTx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				uStakerTx.Validator.NodeID,
				err,
			)
		}
		vdrTx, _ := vdr.AddValidatorTx()

		// Calculate split of reward between delegator/delegatee
		// The delegator gives stake to the validatee
		delegatorShares := reward.PercentDenominator - uint64(vdrTx.Shares)             // parentTx.Shares <= reward.PercentDenominator so no underflow
		delegatorReward := delegatorShares * (stakerReward / reward.PercentDenominator) // delegatorShares <= reward.PercentDenominator so no overflow
		// Delay rounding as long as possible for small numbers
		if optimisticReward, err := math.Mul64(delegatorShares, stakerReward); err == nil {
			delegatorReward = optimisticReward / reward.PercentDenominator
		}
		delegateeReward := stakerReward - delegatorReward // delegatorReward <= reward so no underflow

		offset := 0

		// Reward the delegator here
		if delegatorReward > 0 {
			outIntf, err := e.Fx.CreateOutput(delegatorReward, uStakerTx.RewardsOwner)
			if err != nil {
				return fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return errInvalidState
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: e.Ctx.AVAXAssetID},
				Out:   out,
			}

			e.OnCommit.AddUTXO(utxo)
			e.OnCommit.AddRewardUTXO(tx.TxID, utxo)

			offset++
		}

		// Reward the delegatee here
		if delegateeReward > 0 {
			outIntf, err := e.Fx.CreateOutput(delegateeReward, vdrTx.RewardsOwner)
			if err != nil {
				return fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return errInvalidState
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake) + offset),
				},
				Asset: avax.Asset{ID: e.Ctx.AVAXAssetID},
				Out:   out,
			}

			e.OnCommit.AddUTXO(utxo)
			e.OnCommit.AddRewardUTXO(tx.TxID, utxo)
		}

		nodeID = uStakerTx.Validator.ID()
		startTime = vdrTx.StartTime()
	default:
		return errShouldBeDSValidator
	}

	uptime, err := e.UptimeMan.CalculateUptimePercentFrom(nodeID, startTime)
	if err != nil {
		return fmt.Errorf("failed to calculate uptime: %w", err)
	}

	e.PrefersCommit = uptime >= e.Cfg.UptimePercentage
	return nil
}
