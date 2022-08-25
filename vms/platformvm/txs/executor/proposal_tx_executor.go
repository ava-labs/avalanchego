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

	errWeightTooSmall                    = errors.New("weight of this validator is too low")
	errWeightTooLarge                    = errors.New("weight of this validator is too large")
	errStakeTooShort                     = errors.New("staking period is too short")
	errStakeTooLong                      = errors.New("staking period is too long")
	errInsufficientDelegationFee         = errors.New("staker charges an insufficient delegation fee")
	errFutureStakeTime                   = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	errChildBlockNotAfterParent          = errors.New("proposed timestamp not after current chain time")
	errWrongNumberOfCredentials          = errors.New("should have the same number of credentials as inputs")
	errValidatorSubset                   = errors.New("all subnets' staking period must be a subset of the primary network")
	errStakeOverflow                     = errors.New("validator stake exceeds limit")
	errInvalidState                      = errors.New("generated output isn't valid state")
	errOverDelegated                     = errors.New("validator would be over delegated")
	errShouldBeDSValidator               = errors.New("expected validator to be in the primary network")
	errWrongTxType                       = errors.New("wrong transaction type")
	errInvalidID                         = errors.New("invalid ID")
	errEmptyNodeID                       = errors.New("validator nodeID cannot be empty")
	errAdvanceTimeTxIssuedAfterBlueberry = errors.New("AdvanceTimeTx issued after Blueberry")
)

type ProposalTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	Tx *txs.Tx
	// [OnCommitState] is the state used for validation.
	// In practice, both [OnCommitState] and [onAbortState] are
	// identical when passed into this struct, so we could use either.
	// [OnCommitState] is modified by this struct's methods to
	// reflect changes made to the state if the proposal is committed.
	OnCommitState state.Diff
	// [OnAbortState] is modified by this struct's methods to
	// reflect changes made to the state if the proposal is aborted.
	OnAbortState state.Diff

	// outputs populated by this struct's methods:
	//
	// [PrefersCommit] is true iff this node initially prefers to
	// commit this block transaction.
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

	currentTimestamp := e.OnCommitState.GetTimestamp()

	// Blueberry disallows creating a validator with the empty ID.
	if e.Config.IsBlueberryActivated(currentTimestamp) {
		if tx.Validator.NodeID == ids.EmptyNodeID {
			return errEmptyNodeID
		}
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if e.Bootstrapped.GetValue() {
		// Ensure the proposed validator starts after the current time
		startTime := tx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return fmt.Errorf(
				"validator's start time (%s) at or before current timestamp (%s)",
				startTime,
				currentTimestamp,
			)
		}

		_, err := GetValidator(e.OnCommitState, constants.PrimaryNetworkID, tx.Validator.NodeID)
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
			e.OnCommitState,
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
	// Consume the UTXOS
	utxo.Consume(e.OnCommitState, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnCommitState, txID, tx.Outs)

	newStaker := state.NewPrimaryNetworkStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.PrimaryNetworkValidatorPendingPriority
	e.OnCommitState.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOS
	utxo.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnAbortState, txID, outs)

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

	if e.Bootstrapped.GetValue() {
		currentTimestamp := e.OnCommitState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"validator's start time (%s) is at or after current chain timestamp (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		_, err := GetValidator(e.OnCommitState, tx.Validator.Subnet, tx.Validator.NodeID)
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

		primaryNetworkValidator, err := GetValidator(e.OnCommitState, constants.PrimaryNetworkID, tx.Validator.NodeID)
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

		subnetIntf, _, err := e.OnCommitState.GetTx(tx.Validator.Subnet)
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
			e.OnCommitState,
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
	// Consume the UTXOS
	utxo.Consume(e.OnCommitState, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnCommitState, txID, tx.Outs)

	newStaker := state.NewSubnetStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.SubnetValidatorPendingPriority
	e.OnCommitState.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOS
	utxo.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnAbortState, txID, tx.Outs)

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

	txID := e.Tx.ID()

	newStaker := state.NewPrimaryNetworkStaker(txID, &tx.Validator)
	newStaker.NextTime = newStaker.StartTime
	newStaker.Priority = state.PrimaryNetworkDelegatorPendingPriority

	if e.Bootstrapped.GetValue() {
		currentTimestamp := e.OnCommitState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := tx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		primaryNetworkValidator, err := GetValidator(e.OnCommitState, constants.PrimaryNetworkID, tx.Validator.NodeID)
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

		if e.Config.IsApricotPhase3Activated(currentTimestamp) {
			maximumWeight = math.Min64(maximumWeight, e.Config.MaxValidatorStake)
		}

		canDelegate, err := canDelegate(e.OnCommitState, primaryNetworkValidator, maximumWeight, newStaker)
		if err != nil {
			return err
		}
		if !canDelegate {
			return errOverDelegated
		}

		// Verify the flowcheck
		if err := e.FlowChecker.VerifySpend(
			tx,
			e.OnCommitState,
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
	// Consume the UTXOS
	utxo.Consume(e.OnCommitState, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnCommitState, txID, tx.Outs)

	e.OnCommitState.PutPendingDelegator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOS
	utxo.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.OnAbortState, txID, outs)

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

	// Validate [newChainTime]
	newChainTime := tx.Timestamp()
	if e.Config.IsBlueberryActivated(newChainTime) {
		return fmt.Errorf(
			"%w: proposed timestamp (%s) >= Blueberry fork time (%s)",
			errAdvanceTimeTxIssuedAfterBlueberry,
			newChainTime,
			e.Config.BlueberryTime,
		)
	}

	parentChainTime := e.OnCommitState.GetTimestamp()
	if !newChainTime.After(parentChainTime) {
		return fmt.Errorf(
			"%w, proposed timestamp (%s), chain time (%s)",
			errChildBlockNotAfterParent,
			parentChainTime,
			parentChainTime,
		)
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	nextStakerChangeTime, err := GetNextStakerChangeTime(e.OnCommitState)
	if err != nil {
		return err
	}

	now := e.Clk.Time()
	if err := VerifyNewChainTime(
		newChainTime,
		nextStakerChangeTime,
		now,
	); err != nil {
		return err
	}

	changes, err := AdvanceTimeTo(e.OnCommitState, newChainTime, e.Backend.Rewards)
	if err != nil {
		return err
	}

	// Update the state if this tx is committed
	e.OnCommitState.SetTimestamp(newChainTime)
	changes.Apply(e.OnCommitState)

	e.PrefersCommit = !newChainTime.After(now.Add(SyncBound))

	// Note that state doesn't change if this proposal is aborted
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

	currentStakerIterator, err := e.OnCommitState.GetCurrentStakerIterator()
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
	currentChainTime := e.OnCommitState.GetTimestamp()
	if !stakerToRemove.EndTime.Equal(currentChainTime) {
		return fmt.Errorf(
			"attempting to remove TxID: %s before their end time %s",
			tx.TxID,
			stakerToRemove.EndTime,
		)
	}

	stakerTx, _, err := e.OnCommitState.GetTx(stakerToRemove.TxID)
	if err != nil {
		return fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply := e.OnAbortState.GetCurrentSupply()
	newSupply, err := math.Sub64(currentSupply, stakerToRemove.PotentialReward)
	if err != nil {
		return err
	}
	e.OnAbortState.SetCurrentSupply(newSupply)

	var (
		nodeID    ids.NodeID
		startTime time.Time
	)
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case *txs.AddValidatorTx:
		e.OnCommitState.DeleteCurrentValidator(stakerToRemove)
		e.OnAbortState.DeleteCurrentValidator(stakerToRemove)

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
			e.OnCommitState.AddUTXO(utxo)
			e.OnAbortState.AddUTXO(utxo)
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

			e.OnCommitState.AddUTXO(utxo)
			e.OnCommitState.AddRewardUTXO(tx.TxID, utxo)
		}

		// Handle reward preferences
		nodeID = uStakerTx.Validator.ID()
		startTime = uStakerTx.StartTime()
	case *txs.AddDelegatorTx:
		e.OnCommitState.DeleteCurrentDelegator(stakerToRemove)
		e.OnAbortState.DeleteCurrentDelegator(stakerToRemove)

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
			e.OnCommitState.AddUTXO(utxo)
			e.OnAbortState.AddUTXO(utxo)
		}

		// We're removing a delegator, so we need to fetch the validator they
		// are delegated to.
		vdrStaker, err := e.OnCommitState.GetCurrentValidator(constants.PrimaryNetworkID, uStakerTx.Validator.NodeID)
		if err != nil {
			return fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				uStakerTx.Validator.NodeID,
				err,
			)
		}

		vdrTxIntf, _, err := e.OnCommitState.GetTx(vdrStaker.TxID)
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

			e.OnCommitState.AddUTXO(utxo)
			e.OnCommitState.AddRewardUTXO(tx.TxID, utxo)

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

			e.OnCommitState.AddUTXO(utxo)
			e.OnCommitState.AddRewardUTXO(tx.TxID, utxo)
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
