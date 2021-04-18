// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	errDelegatorSubset = errors.New("delegator's time range must be a subset of the validator's time range")
	errInvalidState    = errors.New("generated output isn't valid state")
	errCapWeightBroken = errors.New("validator would surpass maximum weight")
	errOverDelegated   = errors.New("validator would be over delegated")

	_ UnsignedProposalTx = &UnsignedAddDelegatorTx{}
	_ TimedTx            = &UnsignedAddDelegatorTx{}
)

// UnsignedAddDelegatorTx is an unsigned addDelegatorTx
type UnsignedAddDelegatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Describes the delegatee
	Validator Validator `serialize:"true" json:"validator"`
	// Where to send staked tokens when done validating
	Stake []*avax.TransferableOutput `serialize:"true" json:"stake"`
	// Where to send staking rewards when done validating
	RewardsOwner verify.Verifiable `serialize:"true" json:"rewardsOwner"`
}

// StartTime of this validator
func (tx *UnsignedAddDelegatorTx) StartTime() time.Time {
	return tx.Validator.StartTime()
}

// EndTime of this validator
func (tx *UnsignedAddDelegatorTx) EndTime() time.Time {
	return tx.Validator.EndTime()
}

// Weight of this validator
func (tx *UnsignedAddDelegatorTx) Weight() uint64 {
	return tx.Validator.Weight()
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddDelegatorTx) Verify(
	ctx *snow.Context,
	c codec.Manager,
	minDelegatorStake uint64,
	minStakeDuration time.Duration,
	maxStakeDuration time.Duration,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < minStakeDuration: // Ensure staking length is not too short
		return errStakeTooShort
	case duration > maxStakeDuration: // Ensure staking length is not too long
		return errStakeTooLong
	}

	if err := tx.BaseTx.Verify(ctx, c); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.RewardsOwner); err != nil {
		return fmt.Errorf("failed to verify validator or rewards owner: %w", err)
	}

	totalStakeWeight := uint64(0)
	for _, out := range tx.Stake {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output verification failed: %w", err)
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
		return fmt.Errorf("delegator weight %d is not equal to total stake weight %d", tx.Validator.Wght, totalStakeWeight)
	case tx.Validator.Wght < minDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return errWeightTooSmall
	}

	// cache that this is valid
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedAddDelegatorTx) SemanticVerify(
	vm *VM,
	parentState versionedState,
	stx *Tx,
) (
	versionedState,
	versionedState,
	func() error,
	func() error,
	TxError,
) {
	// Verify the tx is well-formed
	if err := tx.Verify(
		vm.ctx,
		vm.codec,
		vm.MinDelegatorStake,
		vm.MinStakeDuration,
		vm.MaxStakeDuration,
	); err != nil {
		return nil, nil, nil, nil, permError{err}
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	if vm.bootstrapped {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current timestamp
		if validatorStartTime := tx.StartTime(); !currentTimestamp.Before(validatorStartTime) {
			return nil, nil, nil, nil, permError{
				fmt.Errorf(
					"chain timestamp (%s) not before validator's start time (%s)",
					currentTimestamp,
					validatorStartTime,
				),
			}
		} else if validatorStartTime.After(currentTimestamp.Add(maxFutureStartTime)) {
			return nil, nil, nil, nil, permError{
				fmt.Errorf(
					"validator start time (%s) more than two weeks after current chain timestamp (%s)",
					validatorStartTime,
					currentTimestamp,
				),
			}
		}

		currentValidator, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err != nil && err != database.ErrNotFound {
			return nil, nil, nil, nil, tempError{
				fmt.Errorf(
					"failed to find whether %s is a validator: %w",
					tx.Validator.NodeID,
					err,
				),
			}
		}

		pendingValidator := pendingStakers.GetValidator(tx.Validator.NodeID)
		pendingDelegators := pendingValidator.Delegators()

		// Ensure that the period this delegator delegates is a subset of the
		// time the validator validates.
		if err == nil {
			vdrTx := currentValidator.AddValidatorTx()
			if !vdrTx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
				return nil, nil, nil, nil, permError{errDelegatorSubset}
			}
			vdrWeight := vdrTx.Weight()
			delegatorWeight := currentValidator.DelegatorWeight()
			currentWeight, err := math.Add64(vdrWeight, delegatorWeight)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			maximumWeight, err := safemath.Mul64(5, vdrWeight)
			if err != nil {
				return nil, nil, nil, nil, permError{errStakeOverflow}
			}
			currentDelegators := currentValidator.Delegators()
			canDelegate, err := CanDelegate(currentDelegators, pendingDelegators, tx, currentWeight, maximumWeight)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			if !canDelegate {
				return nil, nil, nil, nil, permError{errOverDelegated}
			}
		} else {
			vdrTx, err := pendingStakers.GetStakerByNodeID(tx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return nil, nil, nil, nil, permError{errDelegatorSubset}
				}
				return nil, nil, nil, nil, tempError{
					fmt.Errorf(
						"failed to find whether %s is a validator: %w",
						tx.Validator.NodeID,
						err,
					),
				}
			}

			if !vdrTx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
				return nil, nil, nil, nil, permError{errDelegatorSubset}
			}
			vdrWeight := vdrTx.Weight()
			maximumWeight, err := safemath.Mul64(5, vdrWeight)
			if err != nil {
				return nil, nil, nil, nil, permError{errStakeOverflow}
			}
			canDelegate, err := CanDelegate(nil, pendingDelegators, tx, vdrWeight, maximumWeight)
			if err != nil {
				return nil, nil, nil, nil, permError{err}
			}
			if !canDelegate {
				return nil, nil, nil, nil, permError{errOverDelegated}
			}
		}

		// Verify the flowcheck
		if err := vm.semanticVerifySpend(parentState, tx, tx.Ins, outs, stx.Creds, 0, vm.ctx.AVAXAssetID); err != nil {
			switch err.(type) {
			case permError:
				return nil, nil, nil, nil, permError{
					fmt.Errorf("failed semanticVerifySpend: %w", err),
				}
			default:
				return nil, nil, nil, nil, tempError{
					fmt.Errorf("failed semanticVerifySpend: %w", err),
				}
			}
		}
	}

	// Set up the state if this tx is committed
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := NewVersionedState(parentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	vm.consumeInputs(onCommitState, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	vm.produceOutputs(onCommitState, txID, tx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := NewVersionedState(parentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	vm.consumeInputs(onAbortState, tx.Ins)
	// Produce the UTXOS
	vm.produceOutputs(onAbortState, txID, outs)

	return onCommitState, onAbortState, nil, nil, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *UnsignedAddDelegatorTx) InitiallyPrefersCommit(vm *VM) bool {
	return tx.StartTime().After(vm.clock.Time())
}

// Creates a new transaction
func (vm *VM) newAddDelegatorTx(
	stakeAmt, // Amount the delegator stakes
	startTime, // Unix time they start delegating
	endTime uint64, // Unix time they stop delegating
	nodeID ids.ShortID, // ID of the node we are delegating to
	rewardAddress ids.ShortID, // Address to send reward to, if applicable
	keys []*crypto.PrivateKeySECP256K1R, // Keys providing the staked tokens
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*Tx, error) {
	ins, unlockedOuts, lockedOuts, signers, err := vm.stake(vm.DB, keys, stakeAmt, 0, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	// Create the tx
	utx := &UnsignedAddDelegatorTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         unlockedOuts,
		}},
		Validator: Validator{
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
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(
		vm.ctx,
		vm.codec,
		vm.MinDelegatorStake,
		vm.MinStakeDuration,
		vm.MaxStakeDuration,
	)
}

func CanDelegate(
	current, // sorted by next end time first
	pending []*UnsignedAddDelegatorTx, // sorted by next start time first
	new *UnsignedAddDelegatorTx,
	currentStake,
	maximumStake uint64,
) (bool, error) {
	// For completeness
	if currentStake > maximumStake {
		return false, nil
	}

	toRemoveHeap := validatorHeap{}
	for _, currentDelegator := range current {
		toRemoveHeap.Add(&currentDelegator.Validator)
	}

	var err error

	i := 0
	for ; i < len(pending); i++ { // Iterates in order of increasing start time
		nextPending := pending[i]

		// If the new delegator is starting before or at the same time as this
		// delegator, then we should add the new delegators stake now.
		if !new.StartTime().After(nextPending.StartTime()) {
			break
		}

		for len(toRemoveHeap) > 0 && !toRemoveHeap.Peek().EndTime().After(nextPending.StartTime()) {
			// TODO: in a future network upgrade, this should be changed to:
			// toRemove := toRemoveHeap.Remove()
			// So that we don't mangle the underlying heap.
			toRemove := toRemoveHeap[0]
			toRemoveHeap = toRemoveHeap[1:]

			currentStake, err = safemath.Sub64(currentStake, toRemove.Wght)
			if err != nil {
				return false, err
			}
		}

		// The new delegator hasn't started yet, so we should add the pending
		// delegator to the current set.
		currentStake, err = math.Add64(currentStake, nextPending.Validator.Wght)
		if err != nil {
			return false, err
		}

		toRemoveHeap.Add(&nextPending.Validator)
	}

	for len(toRemoveHeap) > 0 && !toRemoveHeap.Peek().EndTime().After(new.StartTime()) {
		// TODO: in a future network upgrade, this should be changed to:
		// toRemove := toRemoveHeap.Remove()
		// So that we don't mangle the underlying heap.
		toRemove := toRemoveHeap[0]
		toRemoveHeap = toRemoveHeap[1:]

		currentStake, err = safemath.Sub64(currentStake, toRemove.Wght)
		if err != nil {
			return false, err
		}
	}

	currentStake, err = math.Add64(currentStake, new.Validator.Wght)
	if err != nil {
		return false, err
	}

	if currentStake > maximumStake {
		return false, nil
	}

	for ; i < len(pending); i++ { // Iterates in order of increasing start time
		nextPending := pending[i]

		// If the new delegator is starting after this delegator, then we don't
		// need to check that the maximum is honored past this point.
		if nextPending.StartTime().After(new.Validator.EndTime()) {
			break
		}

		for len(toRemoveHeap) > 0 && !toRemoveHeap.Peek().EndTime().After(nextPending.StartTime()) {
			// TODO: in a future network upgrade, this should be changed to:
			// toRemove := toRemoveHeap.Remove()
			// So that we don't mangle the underlying heap.
			toRemove := toRemoveHeap[0]
			toRemoveHeap = toRemoveHeap[1:]

			currentStake, err = safemath.Sub64(currentStake, toRemove.Wght)
			if err != nil {
				return false, err
			}
		}

		// The new delegator hasn't stopped yet, so we should add the pending
		// delegator to the current set.
		currentStake, err = math.Add64(currentStake, nextPending.Validator.Wght)
		if err != nil {
			return false, err
		}

		if currentStake > maximumStake {
			return false, nil
		}

		toRemoveHeap.Add(&nextPending.Validator)
	}

	return true, nil
}
