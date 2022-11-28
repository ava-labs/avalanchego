// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	_ txs.Visitor = (*ProposalTxExecutor)(nil)

	errChildBlockNotAfterParent      = errors.New("proposed timestamp not after current chain time")
	errInvalidState                  = errors.New("generated output isn't valid state")
	errShouldBePermissionlessStaker  = errors.New("expected permissionless staker")
	errWrongTxType                   = errors.New("wrong transaction type")
	errInvalidID                     = errors.New("invalid ID")
	errProposedAddStakerTxAfterBanff = errors.New("staker transaction proposed after Banff")
	errAdvanceTimeTxIssuedAfterBanff = errors.New("AdvanceTimeTx issued after Banff")
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

func (*ProposalTxExecutor) CreateChainTx(*txs.CreateChainTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) ImportTx(*txs.ImportTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) ExportTx(*txs.ExportTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return errWrongTxType
}

func (*ProposalTxExecutor) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return errWrongTxType
}

func (e *ProposalTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	// AddValidatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddValidatorTxs must be issued into
	// StandardBlocks.
	currentTimestamp := e.OnCommitState.GetTimestamp()
	if e.Config.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			errProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.Config.BanffTime,
		)
	}

	onAbortOuts, err := verifyAddValidatorTx(
		e.Backend,
		e.OnCommitState,
		e.Tx,
		tx,
	)
	if err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	// Consume the UTXOs
	utxo.Consume(e.OnCommitState, tx.Ins)
	// Produce the UTXOs
	utxo.Produce(e.OnCommitState, txID, tx.Outs)

	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	e.OnCommitState.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	utxo.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOs
	utxo.Produce(e.OnAbortState, txID, onAbortOuts)

	e.PrefersCommit = tx.StartTime().After(e.Clk.Time())
	return nil
}

func (e *ProposalTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	// AddSubnetValidatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddSubnetValidatorTxs must be
	// issued into StandardBlocks.
	currentTimestamp := e.OnCommitState.GetTimestamp()
	if e.Config.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			errProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.Config.BanffTime,
		)
	}

	if err := verifyAddSubnetValidatorTx(
		e.Backend,
		e.OnCommitState,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	// Consume the UTXOs
	utxo.Consume(e.OnCommitState, tx.Ins)
	// Produce the UTXOs
	utxo.Produce(e.OnCommitState, txID, tx.Outs)

	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	e.OnCommitState.PutPendingValidator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	utxo.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOs
	utxo.Produce(e.OnAbortState, txID, tx.Outs)

	e.PrefersCommit = tx.StartTime().After(e.Clk.Time())
	return nil
}

func (e *ProposalTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	// AddDelegatorTx is a proposal transaction until the Banff fork
	// activation. Following the activation, AddDelegatorTxs must be issued into
	// StandardBlocks.
	currentTimestamp := e.OnCommitState.GetTimestamp()
	if e.Config.IsBanffActivated(currentTimestamp) {
		return fmt.Errorf(
			"%w: timestamp (%s) >= Banff fork time (%s)",
			errProposedAddStakerTxAfterBanff,
			currentTimestamp,
			e.Config.BanffTime,
		)
	}

	onAbortOuts, err := verifyAddDelegatorTx(
		e.Backend,
		e.OnCommitState,
		e.Tx,
		tx,
	)
	if err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Set up the state if this tx is committed
	// Consume the UTXOs
	utxo.Consume(e.OnCommitState, tx.Ins)
	// Produce the UTXOs
	utxo.Produce(e.OnCommitState, txID, tx.Outs)

	newStaker, err := state.NewPendingStaker(txID, tx)
	if err != nil {
		return err
	}

	e.OnCommitState.PutPendingDelegator(newStaker)

	// Set up the state if this tx is aborted
	// Consume the UTXOs
	utxo.Consume(e.OnAbortState, tx.Ins)
	// Produce the UTXOs
	utxo.Produce(e.OnAbortState, txID, onAbortOuts)

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
	if e.Config.IsBanffActivated(newChainTime) {
		return fmt.Errorf(
			"%w: proposed timestamp (%s) >= Banff fork time (%s)",
			errAdvanceTimeTxIssuedAfterBanff,
			newChainTime,
			e.Config.BanffTime,
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

	changes, err := AdvanceTimeTo(e.Backend, e.OnCommitState, newChainTime)
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

	primaryNetworkValidator, err := e.OnCommitState.GetCurrentValidator(
		constants.PrimaryNetworkID,
		stakerToRemove.NodeID,
	)
	if err != nil {
		// This should never error because the staker set is in memory and
		// primary network validators are removed last.
		return err
	}

	stakerTx, _, err := e.OnCommitState.GetTx(stakerToRemove.TxID)
	if err != nil {
		return fmt.Errorf("failed to get next removed staker tx: %w", err)
	}

	switch uStakerTx := stakerTx.Unsigned.(type) {
	case txs.ValidatorTx:
		e.OnCommitState.DeleteCurrentValidator(stakerToRemove)
		e.OnAbortState.DeleteCurrentValidator(stakerToRemove)

		stake := uStakerTx.Stake()
		outputs := uStakerTx.Outputs()
		// Invariant: The staked asset must be equal to the reward asset.
		stakeAsset := stake[0].Asset

		// Refund the stake here
		for i, out := range stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(outputs) + i),
				},
				Asset: out.Asset,
				Out:   out.Output(),
			}
			e.OnCommitState.AddUTXO(utxo)
			e.OnAbortState.AddUTXO(utxo)
		}

		// Provide the reward here
		if stakerToRemove.PotentialReward > 0 {
			validationRewardsOwner := uStakerTx.ValidationRewardsOwner()
			outIntf, err := e.Fx.CreateOutput(stakerToRemove.PotentialReward, validationRewardsOwner)
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
					OutputIndex: uint32(len(outputs) + len(stake)),
				},
				Asset: stakeAsset,
				Out:   out,
			}

			e.OnCommitState.AddUTXO(utxo)
			e.OnCommitState.AddRewardUTXO(tx.TxID, utxo)
		}

		// Invariant: A [txs.DelegatorTx] does not also implement the
		//            [txs.ValidatorTx] interface.
	case txs.DelegatorTx:
		e.OnCommitState.DeleteCurrentDelegator(stakerToRemove)
		e.OnAbortState.DeleteCurrentDelegator(stakerToRemove)

		stake := uStakerTx.Stake()
		outputs := uStakerTx.Outputs()
		stakeAsset := stake[0].Asset

		// Refund the stake here
		for i, out := range stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        tx.TxID,
					OutputIndex: uint32(len(outputs) + i),
				},
				Asset: out.Asset,
				Out:   out.Output(),
			}
			e.OnCommitState.AddUTXO(utxo)
			e.OnAbortState.AddUTXO(utxo)
		}

		// We're removing a delegator, so we need to fetch the validator they
		// are delegated to.
		vdrStaker, err := e.OnCommitState.GetCurrentValidator(
			stakerToRemove.SubnetID,
			stakerToRemove.NodeID,
		)
		if err != nil {
			return fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				stakerToRemove.NodeID,
				err,
			)
		}

		vdrTxIntf, _, err := e.OnCommitState.GetTx(vdrStaker.TxID)
		if err != nil {
			return fmt.Errorf(
				"failed to get whether %s is a validator: %w",
				stakerToRemove.NodeID,
				err,
			)
		}

		// Invariant: Delegators must only be able to reference validator
		//            transactions that implement [txs.ValidatorTx]. All
		//            validator transactions implement this interface except the
		//            AddSubnetValidatorTx.
		vdrTx, ok := vdrTxIntf.Unsigned.(txs.ValidatorTx)
		if !ok {
			return errWrongTxType
		}

		// Calculate split of reward between delegator/delegatee
		// The delegator gives stake to the validatee
		validatorShares := vdrTx.Shares()
		delegatorShares := reward.PercentDenominator - uint64(validatorShares)                            // parentTx.Shares <= reward.PercentDenominator so no underflow
		delegatorReward := delegatorShares * (stakerToRemove.PotentialReward / reward.PercentDenominator) // delegatorShares <= reward.PercentDenominator so no overflow
		// Delay rounding as long as possible for small numbers
		if optimisticReward, err := math.Mul64(delegatorShares, stakerToRemove.PotentialReward); err == nil {
			delegatorReward = optimisticReward / reward.PercentDenominator
		}
		delegateeReward := stakerToRemove.PotentialReward - delegatorReward // delegatorReward <= reward so no underflow

		offset := 0

		// Reward the delegator here
		if delegatorReward > 0 {
			rewardsOwner := uStakerTx.RewardsOwner()
			outIntf, err := e.Fx.CreateOutput(delegatorReward, rewardsOwner)
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
					OutputIndex: uint32(len(outputs) + len(stake)),
				},
				Asset: stakeAsset,
				Out:   out,
			}

			e.OnCommitState.AddUTXO(utxo)
			e.OnCommitState.AddRewardUTXO(tx.TxID, utxo)

			offset++
		}

		// Reward the delegatee here
		if delegateeReward > 0 {
			delegationRewardsOwner := vdrTx.DelegationRewardsOwner()
			outIntf, err := e.Fx.CreateOutput(delegateeReward, delegationRewardsOwner)
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
					OutputIndex: uint32(len(outputs) + len(stake) + offset),
				},
				Asset: stakeAsset,
				Out:   out,
			}

			e.OnCommitState.AddUTXO(utxo)
			e.OnCommitState.AddRewardUTXO(tx.TxID, utxo)
		}
	default:
		// Invariant: Permissioned stakers are removed by the advancement of
		//            time and the current chain timestamp is == this staker's
		//            EndTime. This means only permissionless stakers should be
		//            left in the staker set.
		return errShouldBePermissionlessStaker
	}

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply, err := e.OnAbortState.GetCurrentSupply(stakerToRemove.SubnetID)
	if err != nil {
		return err
	}
	newSupply, err := math.Sub(currentSupply, stakerToRemove.PotentialReward)
	if err != nil {
		return err
	}
	e.OnAbortState.SetCurrentSupply(stakerToRemove.SubnetID, newSupply)

	var expectedUptimePercentage float64
	if stakerToRemove.SubnetID != constants.PrimaryNetworkID {
		transformSubnetIntf, err := e.OnCommitState.GetSubnetTransformation(stakerToRemove.SubnetID)
		if err != nil {
			return err
		}
		transformSubnet, ok := transformSubnetIntf.Unsigned.(*txs.TransformSubnetTx)
		if !ok {
			return errIsNotTransformSubnetTx
		}

		expectedUptimePercentage = float64(transformSubnet.UptimeRequirement) / reward.PercentDenominator
	} else {
		expectedUptimePercentage = e.Config.UptimePercentage
	}

	// TODO: calculate subnet uptimes
	uptime, err := e.Uptimes.CalculateUptimePercentFrom(
		primaryNetworkValidator.NodeID,
		primaryNetworkValidator.StartTime,
	)
	if err != nil {
		return fmt.Errorf("failed to calculate uptime: %w", err)
	}

	e.PrefersCommit = uptime >= expectedUptimePercentage
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
			currentMax = math.Max(currentMax, currentWeight)
		}

		var op func(uint64, uint64) (uint64, error)
		if isAdded {
			op = math.Add64
		} else {
			op = math.Sub[uint64]
		}
		currentWeight, err = op(currentWeight, delegator.Weight)
		if err != nil {
			return 0, err
		}
	}
	// Because we assume [startTime] < [endTime], we have advanced time to
	// be at the end of the delegation window. Make sure that the max weight is
	// updated accordingly.
	return math.Max(currentMax, currentWeight), nil
}
