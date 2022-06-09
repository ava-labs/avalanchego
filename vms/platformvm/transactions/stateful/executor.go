// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/timed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	txstate "github.com/ava-labs/avalanchego/vms/platformvm/state/transactions"
	p_utils "github.com/ava-labs/avalanchego/vms/platformvm/utils"
	p_validator "github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

var (
	_ Executor = &executor{}

	ErrOverDelegated             = errors.New("validator would be over delegated")
	ErrWeightTooLarge            = errors.New("weight of this validator is too large")
	ErrStakeTooShort             = errors.New("staking period is too short")
	ErrStakeTooLong              = errors.New("staking period is too long")
	ErrFutureStakeTime           = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	ErrInsufficientDelegationFee = errors.New("staker charges an insufficient delegation fee")
	ErrInvalidID                 = errors.New("invalid ID")
	ErrShouldBeDSValidator       = errors.New("expected validator to be in the primary network")
)

const (
	// maxValidatorWeightFactor is the maximum factor of the validator stake
	// that is allowed to be placed on a validator.
	maxValidatorWeightFactor uint64 = 5

	// Maximum future start time for staking/delegating
	MaxFutureStartTime = 24 * 7 * 2 * time.Hour

	// SyncBound is the synchrony bound used for safe decision making
	SyncBound = 10 * time.Second
)

type Executor interface {
	// Attempts to verify this transaction with the provided txstate.
	SemanticVerify(
		stx *signed.Tx,
		parentState state.Mutable,
	) error

	ExecuteProposal(
		stx *signed.Tx,
		parentState state.Mutable,
	) (state.Versioned, state.Versioned, error)

	ExecuteDecision(
		stx *signed.Tx,
		vs state.Versioned,
	) (func() error, error)

	ExecuteAtomicTx(
		stx *signed.Tx,
		vs state.Versioned,
	) (func() error, error)

	ProposalInitiallyPrefersCommit(utx unsigned.Tx) bool
}

func NewExecutor(
	cfg *config.Config,
	ctx *snow.Context,
	bootstrapped *utils.AtomicBool,
	clk *mockable.Clock,
	fx fx.Fx,
	utxosMan utxos.SpendHandler,
	timeMan uptime.Manager,
	rewards reward.Calculator,
) Executor {
	return &executor{
		cfg:          cfg,
		ctx:          ctx,
		bootstrapped: bootstrapped,
		clk:          clk,
		fx:           fx,
		spendHandler: utxosMan,
		uptimeMan:    timeMan,
		rewards:      rewards,
	}
}

type executor struct {
	cfg          *config.Config
	ctx          *snow.Context
	bootstrapped *utils.AtomicBool
	clk          *mockable.Clock
	fx           fx.Fx
	spendHandler utxos.SpendHandler
	uptimeMan    uptime.Manager
	rewards      reward.Calculator
}

// Attempts to verify this transaction with the provided txstate.
func (e *executor) SemanticVerify(
	stx *signed.Tx,
	parentState state.Mutable,
) error {
	switch utx := stx.Unsigned.(type) {
	case *unsigned.AddDelegatorTx,
		*unsigned.AddValidatorTx,
		*unsigned.AddSubnetValidatorTx:
		startTime := utx.(timed.Tx).StartTime()
		maxLocalStartTime := e.clk.Time().Add(MaxFutureStartTime)
		if startTime.After(maxLocalStartTime) {
			return ErrFutureStakeTime
		}

		_, _, err := e.ExecuteProposal(stx, parentState)
		// We ignore [errFutureStakeTime] here because an advanceTimeTx will be
		// issued before this transaction is issued.
		if errors.Is(err, ErrFutureStakeTime) {
			return nil
		}
		return err

	case *unsigned.AdvanceTimeTx,
		*unsigned.RewardValidatorTx:
		_, _, err := e.ExecuteProposal(stx, parentState)
		return err

	case *unsigned.CreateChainTx,
		*unsigned.CreateSubnetTx:
		vs := state.NewVersioned(
			parentState,
			parentState.CurrentStakerChainState(),
			parentState.PendingStakerChainState(),
		)
		_, err := e.ExecuteDecision(stx, vs)
		return err

	case *unsigned.ExportTx,
		*unsigned.ImportTx:
		vs := state.NewVersioned(
			parentState,
			parentState.CurrentStakerChainState(),
			parentState.PendingStakerChainState(),
		)
		_, err := e.ExecuteAtomicTx(stx, vs)
		return err

	default:
		return fmt.Errorf("tx type %T could not be semantically verified", utx)
	}
}

func (e *executor) ExecuteProposal(
	stx *signed.Tx,
	parentState state.Mutable,
) (state.Versioned, state.Versioned, error) {
	var (
		txID        = stx.ID()
		creds       = stx.Creds
		signedBytes = stx.Bytes()
	)

	switch utx := stx.Unsigned.(type) {
	case *unsigned.AddDelegatorTx:
		return e.executeAddDelegator(parentState, utx, txID, signedBytes, creds)
	case *unsigned.AddValidatorTx:
		return e.executeAddValidator(parentState, utx, txID, signedBytes, creds)
	case *unsigned.AddSubnetValidatorTx:
		return e.executeAddSubnetValidator(parentState, utx, txID, signedBytes, creds)
	case *unsigned.AdvanceTimeTx:
		return e.executeAdvanceTime(parentState, utx, creds)
	case *unsigned.RewardValidatorTx:
		return e.executeRewardValidator(parentState, utx, creds)
	default:
		return nil, nil, fmt.Errorf("expected proposal tx but got %T", utx)
	}
}

func (e *executor) ExecuteDecision(
	stx *signed.Tx,
	vs state.Versioned,
) (func() error, error) {
	var (
		txID        = stx.ID()
		creds       = stx.Creds
		signedBytes = stx.Bytes()
	)

	switch utx := stx.Unsigned.(type) {
	case *unsigned.CreateChainTx:
		return e.executeCreateChain(vs, utx, txID, signedBytes, creds)
	case *unsigned.CreateSubnetTx:
		return e.executeCreateSubnet(vs, utx, txID, signedBytes, creds)
	case *unsigned.ExportTx:
		return e.executeExport(vs, utx, txID, creds)
	case *unsigned.ImportTx:
		return e.executeImport(vs, utx, txID, creds)
	default:
		return nil, fmt.Errorf("expected decision tx but got %T", utx)
	}
}

func (e *executor) ExecuteAtomicTx(
	stx *signed.Tx,
	vs state.Versioned,
) (func() error, error) {
	var (
		txID  = stx.ID()
		creds = stx.Creds
	)

	switch utx := stx.Unsigned.(type) {
	case *unsigned.ExportTx:
		return e.executeExport(vs, utx, txID, creds)
	case *unsigned.ImportTx:
		return e.executeImport(vs, utx, txID, creds)
	default:
		return nil, fmt.Errorf("expected decision tx but got %T", utx)
	}
}

func (e *executor) executeAddDelegator(
	parentState state.Mutable,
	utx *unsigned.AddDelegatorTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, nil, err
	}

	duration := utx.Validator.Duration()
	switch {
	case duration < e.cfg.MinStakeDuration: // Ensure staking length is not too short
		return nil, nil, ErrStakeTooShort
	case duration > e.cfg.MaxStakeDuration: // Ensure staking length is not too long
		return nil, nil, ErrStakeTooLong
	case utx.Validator.Wght < e.cfg.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, nil, p_validator.ErrWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(utx.Outs)+len(utx.Stake))
	copy(outs, utx.Outs)
	copy(outs[len(utx.Outs):], utx.Stake)

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	if e.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := utx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return nil, nil, fmt.Errorf(
				"chain timestamp (%s) not before validator's start time (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		currentValidator, err := currentStakers.GetValidator(utx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				utx.Validator.NodeID,
				err,
			)
		}

		pendingValidator := pendingStakers.GetValidator(utx.Validator.NodeID)
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
			vdrTx, _, err = pendingStakers.GetValidatorTx(utx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return nil, nil, unsigned.ErrDelegatorSubset
				}
				return nil, nil, fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					utx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this delegator delegates is a subset of the
		// time the validator validates.
		if !utx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return nil, nil, unsigned.ErrDelegatorSubset
		}

		// Ensure that the period this delegator delegates wouldn't become over
		// delegated.
		vdrWeight := vdrTx.Weight()
		currentWeight, err := safemath.Add64(vdrWeight, currentDelegatorWeight)
		if err != nil {
			return nil, nil, err
		}

		maximumWeight, err := safemath.Mul64(maxValidatorWeightFactor, vdrWeight)
		if err != nil {
			return nil, nil, api.ErrStakeOverflow
		}

		if !currentTimestamp.Before(e.cfg.ApricotPhase3Time) {
			maximumWeight = safemath.Min64(maximumWeight, e.cfg.MaxValidatorStake)
		}

		canDelegate, err := txstate.CanDelegate(
			currentDelegators,
			pendingDelegators,
			utx,
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
		if err := e.spendHandler.SemanticVerifySpend(
			parentState,
			utx,
			utx.Ins,
			outs,
			creds,
			e.cfg.AddStakerTxFee,
			e.ctx.AVAXAssetID,
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
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	newlyPendingStakers := pendingStakers.AddStaker(stx)

	onCommitState := state.NewVersioned(parentState, currentStakers, newlyPendingStakers)
	utxos.ConsumeInputs(onCommitState, utx.Ins)
	utxos.ProduceOutputs(onCommitState, txID, e.ctx.AVAXAssetID, utx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)
	utxos.ConsumeInputs(onAbortState, utx.Ins)
	utxos.ProduceOutputs(onAbortState, txID, e.ctx.AVAXAssetID, outs)

	return onCommitState, onAbortState, nil
}

func (e *executor) executeAddValidator(
	parentState state.Mutable,
	utx *unsigned.AddValidatorTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	// Verify the tx is well-formed
	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, nil, err
	}

	switch {
	case utx.Validator.Wght < e.cfg.MinValidatorStake: // Ensure validator is staking at least the minimum amount
		return nil, nil, p_validator.ErrWeightTooSmall
	case utx.Validator.Wght > e.cfg.MaxValidatorStake: // Ensure validator isn't staking too much
		return nil, nil, ErrWeightTooLarge
	case utx.Shares < e.cfg.MinDelegationFee:
		return nil, nil, ErrInsufficientDelegationFee
	}

	duration := utx.Validator.Duration()
	switch {
	case duration < e.cfg.MinStakeDuration: // Ensure staking length is not too short
		return nil, nil, ErrStakeTooShort
	case duration > e.cfg.MaxStakeDuration: // Ensure staking length is not too long
		return nil, nil, ErrStakeTooLong
	}

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	outs := make([]*avax.TransferableOutput, len(utx.Outs)+len(utx.Stake))
	copy(outs, utx.Outs)
	copy(outs[len(utx.Outs):], utx.Stake)

	if e.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		startTime := utx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return nil, nil, fmt.Errorf(
				"validator's start time (%s) at or before current timestamp (%s)",
				startTime,
				currentTimestamp,
			)
		}

		// Ensure this validator isn't currently a validator.
		_, err := currentStakers.GetValidator(utx.Validator.NodeID)
		if err == nil {
			return nil, nil, fmt.Errorf(
				"%s is already a primary network validator",
				utx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				utx.Validator.NodeID,
				err,
			)
		}

		// Ensure this validator isn't about to become a validator.
		_, _, err = pendingStakers.GetValidatorTx(utx.Validator.NodeID)
		if err == nil {
			return nil, nil, fmt.Errorf(
				"%s is about to become a primary network validator",
				utx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is about to become a validator: %w",
				utx.Validator.NodeID,
				err,
			)
		}

		// Verify the flowcheck
		if err := e.spendHandler.SemanticVerifySpend(
			parentState,
			utx,
			utx.Ins,
			outs,
			creds,
			e.cfg.AddStakerTxFee,
			e.ctx.AVAXAssetID,
		); err != nil {
			return nil, nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if startTime.After(maxStartTime) {
			return nil, nil, ErrFutureStakeTime
		}
	}

	// Set up the state if this tx is committed
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := state.NewVersioned(parentState, currentStakers, newlyPendingStakers)

	utxos.ConsumeInputs(onCommitState, utx.Ins)
	utxos.ProduceOutputs(onCommitState, txID, e.ctx.AVAXAssetID, utx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)
	utxos.ConsumeInputs(onAbortState, utx.Ins)
	utxos.ProduceOutputs(onAbortState, txID, e.ctx.AVAXAssetID, outs)

	return onCommitState, onAbortState, nil
}

func (e *executor) executeAddSubnetValidator(
	parentState state.Mutable,
	utx *unsigned.AddSubnetValidatorTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	// Verify the tx is well-formed
	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, nil, err
	}

	duration := utx.Validator.Duration()
	switch {
	case duration < e.cfg.MinStakeDuration: // Ensure staking length is not too short
		return nil, nil, ErrStakeTooShort
	case duration > e.cfg.MaxStakeDuration: // Ensure staking length is not too long
		return nil, nil, ErrStakeTooLong
	case len(creds) == 0:
		return nil, nil, unsigned.ErrWrongNumberOfCredentials
	}

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	if e.bootstrapped.GetValue() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		validatorStartTime := utx.StartTime()
		if !currentTimestamp.Before(validatorStartTime) {
			return nil, nil, fmt.Errorf(
				"validator's start time (%s) is at or after current chain timestamp (%s)",
				currentTimestamp,
				validatorStartTime,
			)
		}

		currentValidator, err := currentStakers.GetValidator(utx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				utx.Validator.NodeID,
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
			if _, validates := subnets[utx.Validator.Subnet]; validates {
				return nil, nil, fmt.Errorf(
					"already validating subnet %s",
					utx.Validator.Subnet,
				)
			}
		} else {
			// This validator is attempting to validate with a node that hasn't
			// started validating yet.
			vdrTx, _, err = pendingStakers.GetValidatorTx(utx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return nil, nil, unsigned.ErrDSValidatorSubset
				}
				return nil, nil, fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					utx.Validator.NodeID,
					err,
				)
			}
		}

		// Ensure that the period this validator validates the specified subnet
		// is a subset of the time they validate the primary network.
		if !utx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return nil, nil, unsigned.ErrDSValidatorSubset
		}

		// Ensure that this transaction isn't a duplicate add validator tx.
		pendingValidator := pendingStakers.GetValidator(utx.Validator.NodeID)
		subnets := pendingValidator.SubnetValidators()
		if _, validates := subnets[utx.Validator.Subnet]; validates {
			return nil, nil, fmt.Errorf(
				"already validating subnet %s",
				utx.Validator.Subnet,
			)
		}

		baseTxCredsLen := len(creds) - 1
		baseTxCreds := creds[:baseTxCredsLen]
		subnetCred := creds[baseTxCredsLen]

		subnetIntf, _, err := parentState.GetTx(utx.Validator.Subnet)
		if err != nil {
			if err == database.ErrNotFound {
				return nil, nil, unsigned.ErrDSValidatorSubset
			}
			return nil, nil, fmt.Errorf(
				"couldn't find subnet %s with %w",
				utx.Validator.Subnet,
				err,
			)
		}

		subnet, ok := subnetIntf.Unsigned.(*unsigned.CreateSubnetTx)
		if !ok {
			return nil, nil, fmt.Errorf(
				"%s is not a subnet",
				utx.Validator.Subnet,
			)
		}

		if err := e.fx.VerifyPermission(utx, utx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
			return nil, nil, err
		}

		// Verify the flowcheck
		if err := e.spendHandler.SemanticVerifySpend(
			parentState,
			utx,
			utx.Ins,
			utx.Outs,
			baseTxCreds,
			e.cfg.TxFee,
			e.ctx.AVAXAssetID,
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
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := state.NewVersioned(parentState, currentStakers, newlyPendingStakers)

	utxos.ConsumeInputs(onCommitState, utx.Ins)
	utxos.ProduceOutputs(onCommitState, txID, e.ctx.AVAXAssetID, utx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)
	utxos.ConsumeInputs(onAbortState, utx.Ins)
	utxos.ProduceOutputs(onAbortState, txID, e.ctx.AVAXAssetID, utx.Outs)

	return onCommitState, onAbortState, nil
}

func (e *executor) executeAdvanceTime(
	parentState state.Mutable,
	utx *unsigned.AdvanceTimeTx,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	switch {
	case utx == nil:
		return nil, nil, unsigned.ErrNilTx
	case len(creds) != 0:
		return nil, nil, unsigned.ErrWrongNumberOfCredentials
	}

	txTimestamp := utx.Timestamp()
	localTimestamp := e.clk.Time()
	localTimestampPlusSync := localTimestamp.Add(SyncBound)
	if localTimestampPlusSync.Before(txTimestamp) {
		return nil, nil, fmt.Errorf(
			"proposed time (%s) is too far in the future relative to local time (%s)",
			txTimestamp,
			localTimestamp,
		)
	}

	if chainTimestamp := parentState.GetTimestamp(); !txTimestamp.After(chainTimestamp) {
		return nil, nil, fmt.Errorf(
			"proposed timestamp (%s), not after current timestamp (%s)",
			txTimestamp,
			chainTimestamp,
		)
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	nextStakerChangeTime, err := parentState.GetNextStakerChangeTime()
	if err != nil {
		return nil, nil, err
	}

	if txTimestamp.After(nextStakerChangeTime) {
		return nil, nil, fmt.Errorf(
			"proposed timestamp (%s) later than next staker change time (%s)",
			txTimestamp,
			nextStakerChangeTime,
		)
	}

	currentSupply := parentState.GetCurrentSupply()

	pendingStakers := parentState.PendingStakerChainState()
	toAddValidatorsWithRewardToCurrent := []*txstate.ValidatorReward(nil)
	toAddDelegatorsWithRewardToCurrent := []*txstate.ValidatorReward(nil)
	toAddWithoutRewardToCurrent := []*signed.Tx(nil)
	numToRemoveFromPending := 0

	// Add to the staker set any pending stakers whose start time is at or
	// before the new timestamp. [pendingStakers.Stakers()] is sorted in order
	// of increasing startTime
pendingStakerLoop:
	for _, stakerTx := range pendingStakers.Stakers() {
		switch staker := stakerTx.Unsigned.(type) {
		case *unsigned.AddDelegatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			r := e.rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = safemath.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, err
			}

			toAddDelegatorsWithRewardToCurrent = append(toAddDelegatorsWithRewardToCurrent, &txstate.ValidatorReward{
				AddStakerTx:     stakerTx,
				PotentialReward: r,
			})
			numToRemoveFromPending++
		case *unsigned.AddValidatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			r := e.rewards.Calculate(
				staker.Validator.Duration(),
				staker.Validator.Wght,
				currentSupply,
			)
			currentSupply, err = safemath.Add64(currentSupply, r)
			if err != nil {
				return nil, nil, err
			}

			toAddValidatorsWithRewardToCurrent = append(toAddValidatorsWithRewardToCurrent, &txstate.ValidatorReward{
				AddStakerTx:     stakerTx,
				PotentialReward: r,
			})
			numToRemoveFromPending++
		case *unsigned.AddSubnetValidatorTx:
			if staker.StartTime().After(txTimestamp) {
				break pendingStakerLoop
			}

			// If this staker should already be removed, then we should just
			// never add them.
			if staker.EndTime().After(txTimestamp) {
				toAddWithoutRewardToCurrent = append(toAddWithoutRewardToCurrent, stakerTx)
			}
			numToRemoveFromPending++
		default:
			return nil, nil, fmt.Errorf("expected validator but got %T", stakerTx.Unsigned)
		}
	}
	newlyPendingStakers := pendingStakers.DeleteStakers(numToRemoveFromPending)

	currentStakers := parentState.CurrentStakerChainState()
	numToRemoveFromCurrent := 0

	// Remove from the staker set any subnet validators whose endTime is at or
	// before the new timestamp
currentStakerLoop:
	for _, tx := range currentStakers.Stakers() {
		switch staker := tx.Unsigned.(type) {
		case *unsigned.AddSubnetValidatorTx:
			if staker.EndTime().After(txTimestamp) {
				break currentStakerLoop
			}

			numToRemoveFromCurrent++
		case *unsigned.AddValidatorTx, *unsigned.AddDelegatorTx:
			// We shouldn't be removing any primary network validators here
			break currentStakerLoop
		default:
			return nil, nil, fmt.Errorf("expected tx type *unsigned.AddValidatorTx or *unsigned.AddDelegatorTx but got %T", staker)
		}
	}
	newlyCurrentStakers, err := currentStakers.UpdateStakers(
		toAddValidatorsWithRewardToCurrent,
		toAddDelegatorsWithRewardToCurrent,
		toAddWithoutRewardToCurrent,
		numToRemoveFromCurrent,
	)
	if err != nil {
		return nil, nil, err
	}

	onCommitState := state.NewVersioned(parentState, newlyCurrentStakers, newlyPendingStakers)
	onCommitState.SetTimestamp(txTimestamp)
	onCommitState.SetCurrentSupply(currentSupply)

	// State doesn't change if this proposal is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)

	return onCommitState, onAbortState, nil
}

// The current validating set must have at least one member.
// The next validator to be removed must be the validator specified in this block.
// The next validator to be removed must be have an end time equal to the current
// chain timestamp.
func (e *executor) executeRewardValidator(
	parentState state.Mutable,
	utx *unsigned.RewardValidatorTx,
	creds []verify.Verifiable,
) (state.Versioned, state.Versioned, error) {
	switch {
	case utx == nil:
		return nil, nil, unsigned.ErrNilTx
	case utx.TxID == ids.Empty:
		return nil, nil, ErrInvalidID
	case len(creds) != 0:
		return nil, nil, unsigned.ErrWrongNumberOfCredentials
	}

	currentStakers := parentState.CurrentStakerChainState()
	stakerTx, stakerReward, err := currentStakers.GetNextStaker()
	if err == database.ErrNotFound {
		return nil, nil, fmt.Errorf("failed to get next staker stop time: %w", err)
	}
	if err != nil {
		return nil, nil, err
	}

	stakerID := stakerTx.ID()
	if stakerID != utx.TxID {
		return nil, nil, fmt.Errorf(
			"attempting to remove TxID: %s. Should be removing %s",
			utx.TxID,
			stakerID,
		)
	}

	// Verify that the chain's timestamp is the validator's end time
	currentTime := parentState.GetTimestamp()
	staker, ok := stakerTx.Unsigned.(timed.Tx)
	if !ok {
		return nil, nil, fmt.Errorf("expected tx type timed.Tx but got %T", stakerTx.Unsigned)
	}
	if endTime := staker.EndTime(); !endTime.Equal(currentTime) {
		return nil, nil, fmt.Errorf(
			"attempting to remove TxID: %s before their end time %s",
			utx.TxID,
			endTime,
		)
	}

	newlyCurrentStakers, err := currentStakers.DeleteNextStaker()
	if err != nil {
		return nil, nil, err
	}

	pendingStakers := parentState.PendingStakerChainState()
	onCommitState := state.NewVersioned(parentState, newlyCurrentStakers, pendingStakers)
	onAbortState := state.NewVersioned(parentState, newlyCurrentStakers, pendingStakers)

	// If the reward is aborted, then the current supply should be decreased.
	currentSupply := onAbortState.GetCurrentSupply()
	newSupply, err := safemath.Sub64(currentSupply, stakerReward)
	if err != nil {
		return nil, nil, err
	}
	onAbortState.SetCurrentSupply(newSupply)

	var (
		nodeID    ids.NodeID
		startTime time.Time
	)
	switch uStakerTx := stakerTx.Unsigned.(type) {
	case *unsigned.AddValidatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: e.ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			onCommitState.AddUTXO(utxo)
			onAbortState.AddUTXO(utxo)
		}

		// Provide the reward here
		if stakerReward > 0 {
			outIntf, err := e.fx.CreateOutput(stakerReward, uStakerTx.RewardsOwner)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, fmt.Errorf("expected verify.State but got %T", outIntf)
			}

			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: e.ctx.AVAXAssetID},
				Out:   out,
			}

			onCommitState.AddUTXO(utxo)
			onCommitState.AddRewardUTXO(utx.TxID, utxo)
		}

		// Handle reward preferences
		nodeID = uStakerTx.Validator.ID()
		startTime = uStakerTx.StartTime()
	case *unsigned.AddDelegatorTx:
		// Refund the stake here
		for i, out := range uStakerTx.Stake {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + i),
				},
				Asset: avax.Asset{ID: e.ctx.AVAXAssetID},
				Out:   out.Output(),
			}
			onCommitState.AddUTXO(utxo)
			onAbortState.AddUTXO(utxo)
		}

		// We're removing a delegator, so we need to fetch the validator they
		// are delgated to.
		vdr, err := currentStakers.GetValidator(uStakerTx.Validator.NodeID)
		if err != nil {
			return nil, nil, fmt.Errorf(
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
		if optimisticReward, err := safemath.Mul64(delegatorShares, stakerReward); err == nil {
			delegatorReward = optimisticReward / reward.PercentDenominator
		}
		delegateeReward := stakerReward - delegatorReward // delegatorReward <= reward so no underflow

		offset := 0

		// Reward the delegator here
		if delegatorReward > 0 {
			outIntf, err := e.fx.CreateOutput(delegatorReward, uStakerTx.RewardsOwner)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, fmt.Errorf("expected verify.State but got %T", outIntf)
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake)),
				},
				Asset: avax.Asset{ID: e.ctx.AVAXAssetID},
				Out:   out,
			}

			onCommitState.AddUTXO(utxo)
			onCommitState.AddRewardUTXO(utx.TxID, utxo)

			offset++
		}

		// Reward the delegatee here
		if delegateeReward > 0 {
			outIntf, err := e.fx.CreateOutput(delegateeReward, vdrTx.RewardsOwner)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create output: %w", err)
			}
			out, ok := outIntf.(verify.State)
			if !ok {
				return nil, nil, fmt.Errorf("expected verify.State but got %T", outIntf)
			}
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        utx.TxID,
					OutputIndex: uint32(len(uStakerTx.Outs) + len(uStakerTx.Stake) + offset),
				},
				Asset: avax.Asset{ID: e.ctx.AVAXAssetID},
				Out:   out,
			}

			onCommitState.AddUTXO(utxo)
			onCommitState.AddRewardUTXO(utx.TxID, utxo)
		}

		nodeID = uStakerTx.Validator.ID()
		startTime = vdrTx.StartTime()
	default:
		return nil, nil, ErrShouldBeDSValidator
	}

	uptime, err := e.uptimeMan.CalculateUptimePercentFrom(nodeID, startTime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to calculate uptime: %w", err)
	}
	utx.ShouldPreferCommit = uptime >= e.cfg.UptimePercentage

	return onCommitState, onAbortState, nil
}

func (e *executor) executeCreateChain(
	vs state.Versioned,
	utx *unsigned.CreateChainTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (func() error, error) {
	// Make sure this transaction is well formed.
	if len(creds) == 0 {
		return nil, unsigned.ErrWrongNumberOfCredentials
	}

	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, err
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(creds) - 1
	baseTxCreds := creds[:baseTxCredsLen]
	subnetCred := creds[baseTxCredsLen]

	// Verify the flowcheck
	createBlockchainTxFee := builder.GetCreateBlockchainTxFee(*e.cfg, vs.GetTimestamp())
	if err := e.spendHandler.SemanticVerifySpend(
		vs,
		utx,
		utx.Ins,
		utx.Outs,
		baseTxCreds,
		createBlockchainTxFee,
		e.ctx.AVAXAssetID,
	); err != nil {
		return nil, err
	}

	subnetIntf, _, err := vs.GetTx(utx.SubnetID)
	if err == database.ErrNotFound {
		return nil, fmt.Errorf("%s isn't a known subnet", utx.SubnetID)
	}
	if err != nil {
		return nil, err
	}

	subnet, ok := subnetIntf.Unsigned.(*unsigned.CreateSubnetTx)
	if !ok {
		return nil, fmt.Errorf("%s isn't a subnet", utx.SubnetID)
	}

	// Verify that this chain is authorized by the subnet
	if err := e.fx.VerifyPermission(utx, utx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return nil, err
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, e.ctx.AVAXAssetID, utx.Outs)

	// Attempt to the new chain to the database
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	vs.AddChain(stx)

	// If this proposal is committed and this node is a member of the
	// subnet that validates the blockchain, create the blockchain
	onAccept := func() error { return p_utils.CreateChain(*e.cfg, utx, txID) }
	return onAccept, nil
}

func (e *executor) executeCreateSubnet(
	vs state.Versioned,
	utx *unsigned.CreateSubnetTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (func() error, error) {
	// Make sure this transaction is well formed.
	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, err
	}

	// Verify the flowcheck
	createSubnetTxFee := builder.GetCreateSubnetTxFee(*e.cfg, vs.GetTimestamp())
	if err := e.spendHandler.SemanticVerifySpend(
		vs,
		utx,
		utx.Ins,
		utx.Outs,
		creds,
		createSubnetTxFee,
		e.ctx.AVAXAssetID,
	); err != nil {
		return nil, err
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, e.ctx.AVAXAssetID, utx.Outs)

	// Attempt to the new chain to the database
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	vs.AddSubnet(stx)

	return nil, nil
}

func (e *executor) executeExport(
	vs state.Versioned,
	utx *unsigned.ExportTx,
	txID ids.ID,
	creds []verify.Verifiable,
) (func() error, error) {
	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, err
	}

	outs := make([]*avax.TransferableOutput, len(utx.Outs)+len(utx.ExportedOutputs))
	copy(outs, utx.Outs)
	copy(outs[len(utx.Outs):], utx.ExportedOutputs)

	if e.bootstrapped.GetValue() {
		if err := verify.SameSubnet(e.ctx, utx.DestinationChain); err != nil {
			return nil, err
		}
	}

	// Verify the flowcheck
	if err := e.spendHandler.SemanticVerifySpend(
		vs,
		utx,
		utx.Ins,
		outs,
		creds,
		e.cfg.TxFee,
		e.ctx.AVAXAssetID,
	); err != nil {
		return nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, e.ctx.AVAXAssetID, utx.Outs)
	return nil, nil
}

func (e *executor) executeImport(
	vs state.Versioned,
	utx *unsigned.ImportTx,
	txID ids.ID,
	creds []verify.Verifiable,
) (func() error, error) {
	if err := utx.SyntacticVerify(e.ctx); err != nil {
		return nil, err
	}

	utxosList := make([]*avax.UTXO, len(utx.Ins)+len(utx.ImportedInputs))
	for index, input := range utx.Ins {
		utxo, err := vs.GetUTXO(input.InputID())
		if err != nil {
			return nil, fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err)
		}
		utxosList[index] = utxo
	}

	if e.bootstrapped.GetValue() {
		if err := verify.SameSubnet(e.ctx, utx.SourceChain); err != nil {
			return nil, err
		}

		utxoIDs := make([][]byte, len(utx.ImportedInputs))
		for i, in := range utx.ImportedInputs {
			utxoID := in.UTXOID.InputID()
			utxoIDs[i] = utxoID[:]
		}
		allUTXOBytes, err := e.ctx.SharedMemory.Get(utx.SourceChain, utxoIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get shared memory: %w", err)
		}

		for i, utxoBytes := range allUTXOBytes {
			utxo := &avax.UTXO{}
			if _, err := unsigned.Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return nil, fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxosList[i+len(utx.Ins)] = utxo
		}

		ins := make([]*avax.TransferableInput, len(utx.Ins)+len(utx.ImportedInputs))
		copy(ins, utx.Ins)
		copy(ins[len(utx.Ins):], utx.ImportedInputs)

		if err := e.spendHandler.SemanticVerifySpendUTXOs(
			utx,
			utxosList,
			ins,
			utx.Outs,
			creds,
			e.cfg.TxFee,
			e.ctx.AVAXAssetID,
		); err != nil {
			return nil, err
		}
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, e.ctx.AVAXAssetID, utx.Outs)
	return nil, nil
}

func (e *executor) ProposalInitiallyPrefersCommit(utx unsigned.Tx) bool {
	switch tx := utx.(type) {
	case *unsigned.AddDelegatorTx:
		return tx.StartTime().After(e.clk.Time())
	case *unsigned.AddValidatorTx:
		return tx.StartTime().After(e.clk.Time())
	case *unsigned.AddSubnetValidatorTx:
		return tx.StartTime().After(e.clk.Time())
	case *unsigned.AdvanceTimeTx:
		txTimestamp := tx.Timestamp()
		localTimestamp := e.clk.Time()
		localTimestampPlusSync := localTimestamp.Add(SyncBound)
		return !txTimestamp.After(localTimestampPlusSync)
	case *unsigned.RewardValidatorTx:
		// InitiallyPrefersCommit returns true if this node thinks the validator
		// should receive a staking reward.
		//
		// TODO: A validator should receive a reward only if they are sufficiently
		// responsive and correct during the time they are validating.
		// Right now they receive a reward if they're up (but not necessarily
		// correct and responsive) for a sufficient amount of time
		return tx.ShouldPreferCommit
	}
	panic("FIND A BETTER WAY TO HANDLE THIS")
}
