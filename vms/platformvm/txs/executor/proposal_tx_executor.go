// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
)

const (
	// Maximum future start time for staking/delegating
	MaxFutureStartTime = 24 * 7 * 2 * time.Hour

	// SyncBound is the synchrony bound used for safe decision making
	SyncBound = 10 * time.Second

	MaxValidatorWeightFactor = 5
)

var (
	_ txs.Visitor = &ProposalTxExecutor{}

	errMissingParentState        = errors.New("missing parent state")
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

type ProposalTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	ParentID      ids.ID
	StateVersions state.Versions
	Tx            *txs.Tx

	// outputs of visitor execution
	OnCommit      state.Diff
	OnAbort       state.Diff
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
	case tx.Validator.Wght < e.Config.MinValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall

	case tx.Validator.Wght > e.Config.MaxValidatorStake:
		// Ensure validator isn't staking too much
		return errWeightTooLarge

	case tx.Shares < e.Config.MinDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return errInsufficientDelegationFee
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < e.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > e.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong
	}

	parentState, ok := e.StateVersions.GetState(e.ParentID)
	if !ok {
		return errMissingParentState
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if e.Bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		startTime := tx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return fmt.Errorf(
				"validator's start time (%s) at or before current timestamp (%s)",
				startTime,
				currentTimestamp,
			)
		}

		_, err := GetValidator(parentState, constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err == nil {
			return fmt.Errorf(
				"attempted to issue duplicate validation for %s",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a primary network validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Verify the flowcheck
		if err := e.FlowChecker.VerifySpend(
			tx,
			parentState,
			tx.Ins,
			outs,
			e.Tx.Creds,
			map[ids.ID]uint64{
				e.Ctx.AVAXAssetID: e.Config.AddStakerTxFee,
			},
		); err != nil {
			return fmt.Errorf("failed verifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow the verifier visitor to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if startTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	onCommit, err := state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}
	e.OnCommit = onCommit

	// Consume the UTXOS
	utxo.Consume(e.OnCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnCommit, txID, tx.Outs)

	newStaker := state.NewPrimaryNetworkStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.PrimaryNetworkValidatorPendingPriority
	e.OnCommit.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	onAbort, err := state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}
	e.OnAbort = onAbort

	// Consume the UTXOS
	utxo.Consume(e.OnAbort, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnAbort, txID, outs)

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
	case duration < e.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > e.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong

	case len(e.Tx.Creds) == 0:
		// Ensure there is at least one credential for the subnet authorization
		return errWrongNumberOfCredentials
	}

	parentState, ok := e.StateVersions.GetState(e.ParentID)
	if !ok {
		return errMissingParentState
	}

	if e.Bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"validator's start time (%s) is at or after current chain timestamp (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		_, err := GetValidator(parentState, tx.Validator.Subnet, tx.Validator.NodeID)
		if err == nil {
			return fmt.Errorf(
				"attempted to issue duplicate subnet validation for %s",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return fmt.Errorf(
				"failed to find whether %s is a subnet validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		primaryNetworkValidator, err := GetValidator(parentState, constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to fetch the primary network validator for %s: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		if !tx.Validator.BoundedBy(primaryNetworkValidator.StartTime, primaryNetworkValidator.EndTime) {
			return errValidatorSubset
		}

		baseTxCredsLen := len(e.Tx.Creds) - 1
		baseTxCreds := e.Tx.Creds[:baseTxCredsLen]
		subnetCred := e.Tx.Creds[baseTxCredsLen]

		subnetIntf, _, err := parentState.GetTx(tx.Validator.Subnet)
		if err != nil {
			return fmt.Errorf(
				"couldn't find subnet %q: %w",
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
		if err := e.FlowChecker.VerifySpend(
			tx,
			parentState,
			tx.Ins,
			tx.Outs,
			baseTxCreds,
			map[ids.ID]uint64{
				e.Ctx.AVAXAssetID: e.Config.TxFee,
			},
		); err != nil {
			return err
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow the verifier visitor to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if validatorStartTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	onCommit, err := state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}
	e.OnCommit = onCommit

	// Consume the UTXOS
	utxo.Consume(e.OnCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnCommit, txID, tx.Outs)

	newStaker := state.NewSubnetStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.SubnetValidatorPendingPriority
	e.OnCommit.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	onAbort, err := state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}
	e.OnAbort = onAbort

	// Consume the UTXOS
	utxo.Consume(e.OnAbort, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnAbort, txID, tx.Outs)

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
	case duration < e.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return errStakeTooShort

	case duration > e.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return errStakeTooLong

	case tx.Validator.Wght < e.Config.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	parentState, ok := e.StateVersions.GetState(e.ParentID)
	if !ok {
		return errMissingParentState
	}

	txID := e.Tx.ID()

	newStaker := state.NewPrimaryNetworkStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.PrimaryNetworkDelegatorPendingPriority

	if e.Bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		primaryNetworkValidator, err := GetValidator(parentState, constants.PrimaryNetworkID, tx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to fetch the primary network validator for %s: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		maximumWeight, err := math.Mul64(MaxValidatorWeightFactor, primaryNetworkValidator.Weight)
		if err != nil {
			return errStakeOverflow
		}

		if !currentTimestamp.Before(e.Config.ApricotPhase3Time) {
			maximumWeight = math.Min64(maximumWeight, e.Config.MaxValidatorStake)
		}

		canDelegate, err := canDelegate(parentState, primaryNetworkValidator, maximumWeight, newStaker)
		if err != nil {
			return err
		}
		if !canDelegate {
			return errOverDelegated
		}

		// Verify the flowcheck
		if err := e.FlowChecker.VerifySpend(
			tx,
			parentState,
			tx.Ins,
			outs,
			e.Tx.Creds,
			map[ids.ID]uint64{
				e.Ctx.AVAXAssetID: e.Config.AddStakerTxFee,
			},
		); err != nil {
			return fmt.Errorf("failed verifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow the verifier visitor to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if validatorStartTime.After(maxStartTime) {
			return errFutureStakeTime
		}
	}

	// Set up the state if this tx is committed
	onCommit, err := state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}
	e.OnCommit = onCommit

	// Consume the UTXOS
	utxo.Consume(e.OnCommit, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnCommit, txID, tx.Outs)

	e.OnCommit.PutPendingDelegator(newStaker)

	// Set up the state if this tx is aborted
	onAbort, err := state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}
	e.OnAbort = onAbort

	// Consume the UTXOS
	utxo.Consume(e.OnAbort, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnAbort, txID, outs)

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

	txTimestamp := tx.Timestamp()
	localTimestamp := e.Clk.Time()
	localTimestampPlusSync := localTimestamp.Add(SyncBound)
	if localTimestampPlusSync.Before(txTimestamp) {
		return fmt.Errorf(
			"proposed time (%s) is too far in the future relative to local time (%s)",
			txTimestamp,
			localTimestamp,
		)
	}

	parentState, ok := e.StateVersions.GetState(e.ParentID)
	if !ok {
		return errMissingParentState
	}

	if chainTimestamp := parentState.GetTimestamp(); !txTimestamp.After(chainTimestamp) {
		return fmt.Errorf(
			"proposed timestamp (%s), not after current timestamp (%s)",
			txTimestamp,
			chainTimestamp,
		)
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	nextStakerChangeTime, err := GetNextStakerChangeTime(parentState)
	if err != nil {
		return err
	}

	if txTimestamp.After(nextStakerChangeTime) {
		return fmt.Errorf(
			"proposed timestamp (%s) later than next staker change time (%s)",
			txTimestamp,
			nextStakerChangeTime,
		)
	}

	pendingStakerIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return err
	}

	var (
		currentSupply             = parentState.GetCurrentSupply()
		currentValidatorsToAdd    []*state.Staker
		pendingValidatorsToRemove []*state.Staker
		currentDelegatorsToAdd    []*state.Staker
		pendingDelegatorsToRemove []*state.Staker
	)

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp
	for pendingStakerIterator.Next() {
		stakerToRemove := pendingStakerIterator.Value()
		if stakerToRemove.StartTime.After(txTimestamp) {
			break
		}

		stakerToAdd := *stakerToRemove
		stakerToAdd.NextTime = stakerToRemove.EndTime
		stakerToAdd.Priority = state.PendingToCurrentPriorities[stakerToRemove.Priority]

		switch stakerToRemove.Priority {
		case state.PrimaryNetworkDelegatorPendingPriority:
			potentialReward := e.Rewards.Calculate(
				stakerToRemove.EndTime.Sub(stakerToRemove.StartTime),
				stakerToRemove.Weight,
				currentSupply,
			)
			currentSupply, err = math.Add64(currentSupply, potentialReward)
			if err != nil {
				pendingStakerIterator.Release()
				return err
			}

			stakerToAdd.PotentialReward = potentialReward

			currentDelegatorsToAdd = append(currentDelegatorsToAdd, &stakerToAdd)
			pendingDelegatorsToRemove = append(pendingDelegatorsToRemove, stakerToRemove)
		case state.PrimaryNetworkValidatorPendingPriority:
			potentialReward := e.Rewards.Calculate(
				stakerToRemove.EndTime.Sub(stakerToRemove.StartTime),
				stakerToRemove.Weight,
				currentSupply,
			)
			currentSupply, err = math.Add64(currentSupply, potentialReward)
			if err != nil {
				pendingStakerIterator.Release()
				return err
			}

			stakerToAdd.PotentialReward = potentialReward

			currentValidatorsToAdd = append(currentValidatorsToAdd, &stakerToAdd)
			pendingValidatorsToRemove = append(pendingValidatorsToRemove, stakerToRemove)
		case state.SubnetValidatorPendingPriority:
			// We require that the [txTimestamp] <= [nextStakerChangeTime].
			// Additionally, the minimum stake duration is > 0. This means we
			// know that the staker we are adding here should never be attempted
			// to be removed in the following loop.

			currentValidatorsToAdd = append(currentValidatorsToAdd, &stakerToAdd)
			pendingValidatorsToRemove = append(pendingValidatorsToRemove, stakerToRemove)
		default:
			pendingStakerIterator.Release()
			return fmt.Errorf("expected staker priority got %d", stakerToRemove.Priority)
		}
	}
	pendingStakerIterator.Release()

	currentStakerIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return err
	}

	var currentValidatorsToRemove []*state.Staker
	for currentStakerIterator.Next() {
		stakerToRemove := currentStakerIterator.Value()
		if stakerToRemove.EndTime.After(txTimestamp) {
			break
		}

		priority := stakerToRemove.Priority
		if priority == state.PrimaryNetworkDelegatorCurrentPriority ||
			priority == state.PrimaryNetworkValidatorCurrentPriority {
			// Primary network stakers are removed by the RewardValidatorTx, not
			// an AdvanceTimeTx.
			break
		}

		currentValidatorsToRemove = append(currentValidatorsToRemove, stakerToRemove)
	}
	currentStakerIterator.Release()

	e.OnCommit, err = state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}

	e.OnCommit.SetTimestamp(txTimestamp)
	e.OnCommit.SetCurrentSupply(currentSupply)

	for _, currentValidatorToAdd := range currentValidatorsToAdd {
		e.OnCommit.PutCurrentValidator(currentValidatorToAdd)
	}
	for _, pendingValidatorToRemove := range pendingValidatorsToRemove {
		e.OnCommit.DeletePendingValidator(pendingValidatorToRemove)
	}
	for _, currentDelegatorToAdd := range currentDelegatorsToAdd {
		e.OnCommit.PutCurrentDelegator(currentDelegatorToAdd)
	}
	for _, pendingDelegatorToRemove := range pendingDelegatorsToRemove {
		e.OnCommit.DeletePendingDelegator(pendingDelegatorToRemove)
	}
	for _, currentValidatorToRemove := range currentValidatorsToRemove {
		e.OnCommit.DeleteCurrentValidator(currentValidatorToRemove)
	}

	// State doesn't change if this proposal is aborted
	e.OnAbort, err = state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}

	e.PrefersCommit = !txTimestamp.After(localTimestampPlusSync)
	return nil
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

	parentState, ok := e.StateVersions.GetState(e.ParentID)
	if !ok {
		return errMissingParentState
	}

	currentStakerIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return err
	}
	if !currentStakerIterator.Next() {
		return fmt.Errorf("failed to get next staker to remove: %w", database.ErrNotFound)
	}
	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	if stakerToRemove.TxID != tx.TxID {
		return fmt.Errorf(
			"attempting to remove TxID: %s. Should be removing %s",
			tx.TxID,
			stakerToRemove.TxID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentChainTime := parentState.GetTimestamp()
	if !stakerToRemove.EndTime.Equal(currentChainTime) {
		return fmt.Errorf(
			"attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			stakerToRemove.EndTime,
		)
	}

	stakerTx, _, err := parentState.GetTx(stakerToRemove.TxID)
	if err != nil {
		return fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	e.OnCommit, err = state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}

	e.OnAbort, err = state.NewDiff(e.ParentID, e.StateVersions)
	if err != nil {
		return err
	}

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply := e.OnAbort.GetCurrentSupply()
	newSupply, err := math.Sub64(currentSupply, stakerToRemove.PotentialReward)
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
		e.OnCommit.DeleteCurrentValidator(stakerToRemove)
		e.OnAbort.DeleteCurrentValidator(stakerToRemove)

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
		if stakerToRemove.PotentialReward > 0 {
			outIntf, err := e.Fx.CreateOutput(stakerToRemove.PotentialReward, uStakerTx.RewardsOwner)
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
		e.OnCommit.DeleteCurrentDelegator(stakerToRemove)
		e.OnAbort.DeleteCurrentDelegator(stakerToRemove)

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
		// are delegated to.
		vdrStaker, err := parentState.GetCurrentValidator(constants.PrimaryNetworkID, uStakerTx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				uStakerTx.Validator.NodeID,
				err,
			)
		}

		vdrTxIntf, _, err := parentState.GetTx(vdrStaker.TxID)
		if err != nil {
			return fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				uStakerTx.Validator.NodeID,
				err,
			)
		}

		vdrTx, ok := vdrTxIntf.Unsigned.(*txs.AddValidatorTx)
		if !ok {
			return errWrongTxType
		}

		// Calculate split of reward between delegator/delegatee
		// The delegator gives stake to the validatee
		delegatorShares := reward.PercentDenominator - uint64(vdrTx.Shares)                               // parentTx.Shares <= reward.PercentDenominator so no underflow
		delegatorReward := delegatorShares * (stakerToRemove.PotentialReward / reward.PercentDenominator) // delegatorShares <= reward.PercentDenominator so no overflow
		// Delay rounding as long as possible for small numbers
		if optimisticReward, err := math.Mul64(delegatorShares, stakerToRemove.PotentialReward); err == nil {
			delegatorReward = optimisticReward / reward.PercentDenominator
		}
		delegateeReward := stakerToRemove.PotentialReward - delegatorReward // delegatorReward <= reward so no underflow

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

	uptime, err := e.Uptimes.CalculateUptimePercentFrom(nodeID, startTime)
	if err != nil {
		return fmt.Errorf("failed to calculate uptime: %w", err)
	}

	e.PrefersCommit = uptime >= e.Config.UptimePercentage
	return nil
}

// GetNextStakerChangeTime returns the next time a staker will be either added
// or removed to/from the current validator set.
func GetNextStakerChangeTime(state state.Chain) (time.Time, error) {
	currentStakerIterator, err := state.GetCurrentStakerIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer currentStakerIterator.Release()

	pendingStakerIterator, err := state.GetPendingStakerIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer pendingStakerIterator.Release()

	hasCurrentStaker := currentStakerIterator.Next()
	hasPendingStaker := pendingStakerIterator.Next()
	switch {
	case hasCurrentStaker && hasPendingStaker:
		nextCurrentTime := currentStakerIterator.Value().NextTime
		nextPendingTime := pendingStakerIterator.Value().NextTime
		if nextCurrentTime.Before(nextPendingTime) {
			return nextCurrentTime, nil
		}
		return nextPendingTime, nil
	case hasCurrentStaker:
		return currentStakerIterator.Value().NextTime, nil
	case hasPendingStaker:
		return pendingStakerIterator.Value().NextTime, nil
	default:
		return time.Time{}, database.ErrNotFound
	}
}

// GetValidator returns information about the given validator, which may be a
// current validator or pending validator.
func GetValidator(state state.Chain, subnetID ids.ID, nodeID ids.NodeID) (*state.Staker, error) {
	validator, err := state.GetCurrentValidator(subnetID, nodeID)
	if err == nil {
		// This node is currently validating the subnet.
		return validator, nil
	}
	if err != database.ErrNotFound {
		// Unexpected error occurred.
		return nil, err
	}
	return state.GetPendingValidator(subnetID, nodeID)
}

// canDelegate returns true if [delegator] can be added as a delegator of
// [validator].
//
// A [delegator] can be added if:
// - [delegator]'s start time is not before [validator]'s start time
// - [delegator]'s end time is not after [validator]'s end time
// - the maximum total weight on [validator] will not exceed [weightLimit]
func canDelegate(
	state state.Chain,
	validator *state.Staker,
	weightLimit uint64,
	delegator *state.Staker,
) (bool, error) {
	if delegator.StartTime.Before(validator.StartTime) {
		return false, nil
	}
	if delegator.EndTime.After(validator.EndTime) {
		return false, nil
	}

	maxWeight, err := GetMaxWeight(state, validator, delegator.StartTime, delegator.EndTime)
	if err != nil {
		return false, err
	}
	newMaxWeight, err := math.Add64(maxWeight, delegator.Weight)
	if err != nil {
		return false, err
	}
	return newMaxWeight <= weightLimit, nil
}

// GetMaxWeight returns the maximum total weight of the [validator], including
// its own weight, between [startTime] and [endTime].
// The weight changes are applied in the order they will be applied as chain
// time advances.
// Invariant:
// - [validator.StartTime] <= [startTime] < [endTime] <= [validator.EndTime]
func GetMaxWeight(
	chainState state.Chain,
	validator *state.Staker,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	currentDelegatorIterator, err := chainState.GetCurrentDelegatorIterator(validator.SubnetID, validator.NodeID)
	if err != nil {
		return 0, err
	}

	// TODO: We can optimize this by moving the current total weight to be
	//       stored in the validator state.
	//
	// Calculate the current total weight on this validator, including the
	// weight of the actual validator and the sum of the weights of all of the
	// currently active delegators.
	currentWeight := validator.Weight
	for currentDelegatorIterator.Next() {
		currentDelegator := currentDelegatorIterator.Value()

		currentWeight, err = math.Add64(currentWeight, currentDelegator.Weight)
		if err != nil {
			currentDelegatorIterator.Release()
			return 0, err
		}
	}
	currentDelegatorIterator.Release()

	currentDelegatorIterator, err = chainState.GetCurrentDelegatorIterator(validator.SubnetID, validator.NodeID)
	if err != nil {
		return 0, err
	}
	pendingDelegatorIterator, err := chainState.GetPendingDelegatorIterator(validator.SubnetID, validator.NodeID)
	if err != nil {
		currentDelegatorIterator.Release()
		return 0, err
	}
	delegatorChangesIterator := state.NewStakerDiffIterator(currentDelegatorIterator, pendingDelegatorIterator)
	defer delegatorChangesIterator.Release()

	// Iterate over the future stake weight changes and calculate the maximum
	// total weight on the validator, only including the points in the time
	// range [startTime, endTime].
	var currentMax uint64
	for delegatorChangesIterator.Next() {
		delegator, isAdded := delegatorChangesIterator.Value()
		// [delegator.NextTime] > [endTime]
		if delegator.NextTime.After(endTime) {
			// This delegation change (and all following changes) occurs after
			// [endTime]. Since we're calculating the max amount staked in
			// [startTime, endTime], we can stop.
			break
		}

		// [delegator.NextTime] >= [startTime]
		if !delegator.NextTime.Before(startTime) {
			// We have advanced time to be at the inside of the delegation
			// window. Make sure that the max weight is updated accordingly.
			currentMax = math.Max64(currentMax, currentWeight)
		}

		var op func(uint64, uint64) (uint64, error)
		if isAdded {
			op = math.Add64
		} else {
			op = math.Sub64
		}
		currentWeight, err = op(currentWeight, delegator.Weight)
		if err != nil {
			return 0, err
		}
	}
	// Because we assume [startTime] < [endTime], we have advanced time to
	// be at the end of the delegation window. Make sure that the max weight is
	// updated accordingly.
	return math.Max64(currentMax, currentWeight), nil
}
