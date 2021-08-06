// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"container/heap"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errDelegatorSubset = errors.New("delegator's time range must be a subset of the validator's time range")
	errInvalidState    = errors.New("generated output isn't valid state")
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
		newWeight, err := math.Add64(totalStakeWeight, out.Output().Amount())
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
	parentState MutableState,
	stx *Tx,
) (
	VersionedState,
	VersionedState,
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
					tx.Validator.NodeID.PrefixedString(constants.NodeIDPrefix),
					err,
				),
			}
		}

		pendingValidator := pendingStakers.GetValidator(tx.Validator.NodeID)
		pendingDelegators := pendingValidator.Delegators()

		var (
			vdrTx                  *UnsignedAddValidatorTx
			currentDelegatorWeight uint64
			currentDelegators      []*UnsignedAddDelegatorTx
		)
		if err == nil {
			// This delegator is attempting to delegate to a currently validing
			// node.
			vdrTx = currentValidator.AddValidatorTx()
			currentDelegatorWeight = currentValidator.DelegatorWeight()
			currentDelegators = currentValidator.Delegators()
		} else {
			// This delegator is attempting to delegate to a node that hasn't
			// started validating yet.
			vdrTx, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
			if err != nil {
				if err == database.ErrNotFound {
					return nil, nil, nil, nil, permError{errDelegatorSubset}
				}
				return nil, nil, nil, nil, tempError{
					fmt.Errorf(
						"failed to find whether %s is a validator: %w",
						tx.Validator.NodeID.PrefixedString(constants.NodeIDPrefix),
						err,
					),
				}
			}
		}

		// Ensure that the period this delegator delegates is a subset of the
		// time the validator validates.
		if !tx.Validator.BoundedBy(vdrTx.StartTime(), vdrTx.EndTime()) {
			return nil, nil, nil, nil, permError{errDelegatorSubset}
		}

		// Ensure that the period this delegator delegates wouldn't become over
		// delegated.
		vdrWeight := vdrTx.Weight()
		currentWeight, err := math.Add64(vdrWeight, currentDelegatorWeight)
		if err != nil {
			return nil, nil, nil, nil, permError{err}
		}

		maximumWeight, err := math.Mul64(MaxValidatorWeightFactor, vdrWeight)
		if err != nil {
			return nil, nil, nil, nil, permError{errStakeOverflow}
		}

		isAP3 := !currentTimestamp.Before(vm.ApricotPhase3Time)
		if isAP3 {
			maximumWeight = math.Min64(maximumWeight, vm.MaxValidatorStake)
		}

		canDelegate, err := CanDelegate(
			currentDelegators,
			pendingDelegators,
			tx,
			currentWeight,
			maximumWeight,
			isAP3,
		)
		if err != nil {
			return nil, nil, nil, nil, permError{err}
		}
		if !canDelegate {
			return nil, nil, nil, nil, permError{errOverDelegated}
		}

		// Verify the flowcheck
		if err := vm.semanticVerifySpend(parentState, tx, tx.Ins, outs, stx.Creds, vm.AddStakerTxFee, vm.ctx.AVAXAssetID); err != nil {
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
	ins, unlockedOuts, lockedOuts, signers, err := vm.stake(keys, stakeAmt, vm.AddStakerTxFee, changeAddr)
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

// CanDelegate returns if the [new] delegator can be added to a validator who
// has [current] and [pending] delegators. [currentStake] is the current amount
// of stake on the validator, include the [current] delegators. [maximumStake]
// is the maximum amount of stake that can be on the validator at any given
// time. It is assumed that the validator without adding [new] does not violate
// [maximumStake]. It is assumed that [pending]
func CanDelegate(
	current,
	pending []*UnsignedAddDelegatorTx, // sorted by next start time first
	new *UnsignedAddDelegatorTx,
	currentStake,
	maximumStake uint64,
	useHeapCorrectly bool, // TODO: this should be removed after AP3 is live
) (bool, error) {
	var (
		maxStake uint64
		err      error
	)
	if useHeapCorrectly {
		maxStake, err = FixedMaxStakeAmount(current, pending, new.StartTime(), new.EndTime(), currentStake)
	} else {
		maxStake, err = MaxStakeAmount(current, pending, new.StartTime(), new.EndTime(), currentStake)
	}
	if err != nil {
		return false, err
	}
	newMaxStake, err := math.Add64(maxStake, new.Validator.Wght)
	if err != nil {
		return false, err
	}
	return newMaxStake <= maximumStake, nil
}

// TODO: this should be removed after AP3 is live
func MaxStakeAmount(
	current,
	pending []*UnsignedAddDelegatorTx, // sorted by next start time first
	startTime time.Time,
	endTime time.Time,
	currentStake uint64,
) (uint64, error) {
	// Keep track of which delegators should be removed next so that we can
	// efficiently remove delegators and keep the current stake updated.
	toRemoveHeap := validatorHeap{}
	for _, currentDelegator := range current {
		toRemoveHeap.Add(&currentDelegator.Validator)
	}

	var (
		err      error
		maxStake uint64 = 0
	)

	// Iterate through time until [endTime].
	for _, nextPending := range pending { // Iterates in order of increasing start time
		nextPendingStartTime := nextPending.StartTime()

		// If the new delegator is starting after [endTime], then we don't need
		// to check the maximum after this point.
		if nextPendingStartTime.After(endTime) {
			break
		}

		for len(toRemoveHeap) > 0 {
			toRemoveEndTime := toRemoveHeap.Peek().EndTime()
			if toRemoveEndTime.After(nextPendingStartTime) {
				break
			}

			if !toRemoveEndTime.Before(startTime) && currentStake > maxStake {
				maxStake = currentStake
			}

			toRemove := toRemoveHeap[0]
			toRemoveHeap = toRemoveHeap[1:]

			currentStake, err = math.Sub64(currentStake, toRemove.Wght)
			if err != nil {
				return 0, err
			}
		}

		// The new delegator hasn't stopped yet, so we should add the pending
		// delegator to the current set.
		currentStake, err = math.Add64(currentStake, nextPending.Validator.Wght)
		if err != nil {
			return 0, err
		}

		if currentStake > maxStake {
			maxStake = currentStake
		}

		toRemoveHeap.Add(&nextPending.Validator)
	}

	for len(toRemoveHeap) > 0 {
		toRemoveEndTime := toRemoveHeap.Peek().EndTime()
		if !toRemoveEndTime.Before(startTime) {
			break
		}

		toRemove := toRemoveHeap[0]
		toRemoveHeap = toRemoveHeap[1:]

		currentStake, err = math.Sub64(currentStake, toRemove.Wght)
		if err != nil {
			return 0, err
		}
	}

	if currentStake > maxStake {
		maxStake = currentStake
	}

	return maxStake, nil
}

func FixedMaxStakeAmount(
	current,
	pending []*UnsignedAddDelegatorTx, // sorted by next start time first
	startTime time.Time,
	endTime time.Time,
	currentStake uint64,
) (uint64, error) {
	// Keep track of which delegators should be removed next so that we can
	// efficiently remove delegators and keep the current stake updated.
	toRemoveHeap := validatorHeap{}
	for _, currentDelegator := range current {
		toRemoveHeap.Add(&currentDelegator.Validator)
	}

	var (
		err      error
		maxStake uint64 = 0
	)

	// Iterate through time until [endTime].
	for _, nextPending := range pending { // Iterates in order of increasing start time
		nextPendingStartTime := nextPending.StartTime()

		// If the new delegator is starting after [endTime], then we don't need
		// to check the maximum after this point.
		if nextPendingStartTime.After(endTime) {
			break
		}

		// Remove all the delegators that finish before the next delegator will
		// start.
		for len(toRemoveHeap) > 0 {
			toRemove := toRemoveHeap.Peek()
			toRemoveEndTime := toRemove.EndTime()
			if toRemoveEndTime.After(nextPendingStartTime) {
				break
			}

			// Changed in AP3:
			// If the new delegator has started, then this current delegator
			// should have an end time that is > [startTime].
			newDelegatorHasStartedBeforeFinish := toRemoveEndTime.After(startTime)
			if newDelegatorHasStartedBeforeFinish && currentStake > maxStake {
				maxStake = currentStake
			}

			currentStake, err = math.Sub64(currentStake, toRemove.Wght)
			if err != nil {
				return 0, err
			}

			// Changed in AP3:
			// Remove the delegator from the heap and update the heap so that
			// the top of the heap is the next delegator to remove.
			toRemoveHeap.Remove()
		}

		// The new delegator hasn't stopped yet, so we should add the pending
		// delegator to the current set.
		currentStake, err = math.Add64(currentStake, nextPending.Validator.Wght)
		if err != nil {
			return 0, err
		}

		// Changed in AP3:
		// If the new delegator has started, then this pending delegator should
		// have a start time that is >= [startTime]. Otherwise, the delegator
		// hasn't started yet and the [currentStake] shouldn't count towards the
		// [maximumStake] during the delegators delegation period.
		newDelegatorHasStarted := !nextPendingStartTime.Before(startTime)
		if newDelegatorHasStarted && currentStake > maxStake {
			maxStake = currentStake
		}

		toRemoveHeap.Add(&nextPending.Validator)
	}

	// If there aren't any delegators that will be added before the end of our
	// delegation period, we should advance through time until our delegation
	// period starts.
	for len(toRemoveHeap) > 0 {
		toRemove := toRemoveHeap.Peek()
		toRemoveEndTime := toRemove.EndTime()
		if toRemoveEndTime.After(startTime) {
			break
		}

		currentStake, err = math.Sub64(currentStake, toRemove.Wght)
		if err != nil {
			return 0, err
		}

		// Changed in AP3:
		// Remove the delegator from the heap and update the heap so that the
		// top of the heap is the next delegator to remove.
		toRemoveHeap.Remove()
	}

	// We have advanced time to be inside the delegation window. So, make sure
	// that the max stake is updated accordingly.
	if currentStake > maxStake {
		maxStake = currentStake
	}

	return maxStake, nil
}

func (vm *VM) maxStakeAmount(
	subnetID ids.ID,
	nodeID ids.ShortID,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	if startTime.After(endTime) {
		return 0, errStartAfterEndTime
	}
	if timestamp := vm.internalState.GetTimestamp(); startTime.Before(timestamp) {
		return 0, errStartTimeTooEarly
	}
	if subnetID == constants.PrimaryNetworkID {
		return vm.maxPrimarySubnetStakeAmount(nodeID, startTime, endTime)
	}
	return vm.maxSubnetStakeAmount(subnetID, nodeID, startTime, endTime)
}

func (vm *VM) maxSubnetStakeAmount(
	subnetID ids.ID,
	nodeID ids.ShortID,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	var (
		vdrTx  *UnsignedAddSubnetValidatorTx
		exists bool
	)

	pendingStakers := vm.internalState.PendingStakerChainState()
	pendingValidator := pendingStakers.GetValidator(nodeID)

	currentStakers := vm.internalState.CurrentStakerChainState()
	currentValidator, err := currentStakers.GetValidator(nodeID)
	switch err {
	case nil:
		vdrTx, exists = currentValidator.SubnetValidators()[subnetID]
		if !exists {
			vdrTx = pendingValidator.SubnetValidators()[subnetID]
		}
	case database.ErrNotFound:
		vdrTx = pendingValidator.SubnetValidators()[subnetID]
	default:
		return 0, err
	}

	if vdrTx == nil {
		return 0, nil
	}
	if vdrTx.StartTime().After(endTime) {
		return 0, nil
	}
	if vdrTx.EndTime().Before(startTime) {
		return 0, nil
	}
	return vdrTx.Weight(), nil
}

func (vm *VM) maxPrimarySubnetStakeAmount(
	nodeID ids.ShortID,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	currentStakers := vm.internalState.CurrentStakerChainState()
	pendingStakers := vm.internalState.PendingStakerChainState()

	pendingValidator := pendingStakers.GetValidator(nodeID)
	currentValidator, err := currentStakers.GetValidator(nodeID)

	switch err {
	case nil:
		vdrTx := currentValidator.AddValidatorTx()
		if vdrTx.StartTime().After(endTime) {
			return 0, nil
		}
		if vdrTx.EndTime().Before(startTime) {
			return 0, nil
		}

		currentWeight := vdrTx.Weight()
		currentWeight, err = math.Add64(currentWeight, currentValidator.DelegatorWeight())
		if err != nil {
			return 0, err
		}
		return MaxStakeAmount(
			currentValidator.Delegators(),
			pendingValidator.Delegators(),
			startTime,
			endTime,
			currentWeight,
		)
	case database.ErrNotFound:
		futureValidator, err := pendingStakers.GetValidatorTx(nodeID)
		if err == database.ErrNotFound {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		if futureValidator.StartTime().After(endTime) {
			return 0, nil
		}
		if futureValidator.EndTime().Before(startTime) {
			return 0, nil
		}

		return MaxStakeAmount(
			nil,
			pendingValidator.Delegators(),
			startTime,
			endTime,
			futureValidator.Weight(),
		)
	default:
		return 0, err
	}
}

type validatorHeap []*Validator

func (h *validatorHeap) Len() int                 { return len(*h) }
func (h *validatorHeap) Less(i, j int) bool       { return (*h)[i].EndTime().Before((*h)[j].EndTime()) }
func (h *validatorHeap) Swap(i, j int)            { (*h)[i], (*h)[j] = (*h)[j], (*h)[i] }
func (h *validatorHeap) Add(validator *Validator) { heap.Push(h, validator) }
func (h *validatorHeap) Peek() *Validator         { return (*h)[0] }
func (h *validatorHeap) Remove() *Validator       { return heap.Pop(h).(*Validator) }
func (h *validatorHeap) Push(x interface{})       { *h = append(*h, x.(*Validator)) }
func (h *validatorHeap) Pop() interface{} {
	newLen := len(*h) - 1
	val := (*h)[newLen]
	*h = (*h)[:newLen]
	return val
}
