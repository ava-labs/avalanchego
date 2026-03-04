// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"context"
	"errors"
	"math/bits"
	"time"

	"gonum.org/v1/gonum/mathext/prng"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Proposer list constants
const (
	WindowDuration = 5 * time.Second

	MaxVerifyWindows = 6
	MaxVerifyDelay   = MaxVerifyWindows * WindowDuration // 30 seconds

	MaxBuildWindows = 60
	MaxBuildDelay   = MaxBuildWindows * WindowDuration // 5 minutes

	MaxLookAheadSlots  = 720
	MaxLookAheadWindow = MaxLookAheadSlots * WindowDuration // 1 hour
)

var (
	_ Windower = (*windower)(nil)

	ErrAnyoneCanPropose         = errors.New("anyone can propose")
	ErrUnexpectedSamplerFailure = errors.New("unexpected sampler failure")
)

type Windower interface {
	// Proposers returns the proposer list for building a block at [blockHeight]
	// when the validator set is defined at [pChainHeight]. The list is returned
	// in order. The minimum delay of a validator is the index they appear times
	// [WindowDuration].
	Proposers(
		ctx context.Context,
		blockHeight,
		pChainHeight uint64,
		maxWindows int,
	) ([]ids.NodeID, error)

	// Delay returns the amount of time that [validatorID] must wait before
	// building a block at [blockHeight] when the validator set is defined at
	// [pChainHeight].
	Delay(
		ctx context.Context,
		blockHeight,
		pChainHeight uint64,
		validatorID ids.NodeID,
		maxWindows int,
	) (time.Duration, error)

	// In the Post-Durango windowing scheme, every validator active at
	// [pChainHeight] gets specific slots it can propose in (instead of being
	// able to propose from a given time on as it happens Pre-Durango).
	// [ExpectedProposer] calculates which nodeID is scheduled to propose a
	// block of height [blockHeight] at [slot].
	// If no validators are currently available, [ErrAnyoneCanPropose] is
	// returned.
	ExpectedProposer(
		ctx context.Context,
		blockHeight,
		pChainHeight,
		slot uint64,
	) (ids.NodeID, error)

	// In the Post-Durango windowing scheme, every validator active at
	// [pChainHeight] gets specific slots it can propose in (instead of being
	// able to propose from a given time on as it happens Pre-Durango).
	// [MinDelayForProposer] specifies how long [nodeID] needs to wait for its
	// slot to start. Delay is specified as starting from slot zero start.
	// (which is parent timestamp). For efficiency reasons, we cap the slot
	// search to [MaxLookAheadSlots].
	// If no validators are currently available, [ErrAnyoneCanPropose] is
	// returned.
	MinDelayForProposer(
		ctx context.Context,
		blockHeight,
		pChainHeight uint64,
		nodeID ids.NodeID,
		startSlot uint64,
	) (time.Duration, error)
}

// windower interfaces with P-Chain and it is responsible for calculating the
// delay for the block submission window of a given validator
type windower struct {
	state       validators.State
	subnetID    ids.ID
	chainSource uint64
}

func New(state validators.State, subnetID, chainID ids.ID) Windower {
	w := wrappers.Packer{Bytes: chainID[:]}
	return &windower{
		state:       state,
		subnetID:    subnetID,
		chainSource: w.UnpackLong(),
	}
}

func (w *windower) Proposers(ctx context.Context, blockHeight, pChainHeight uint64, maxWindows int) ([]ids.NodeID, error) {
	// Note: The 32-bit prng is used here for legacy reasons. All other usages
	// of a prng in this file should use the 64-bit version.
	source := prng.NewMT19937()
	sampler, validators, err := w.makeSampler(ctx, pChainHeight, source)
	if err != nil {
		return nil, err
	}

	var totalWeight uint64
	for _, validator := range validators {
		totalWeight, err = math.Add(totalWeight, validator.weight)
		if err != nil {
			return nil, err
		}
	}

	source.Seed(w.chainSource ^ blockHeight)

	numToSample := int(min(uint64(maxWindows), totalWeight))
	indices, ok := sampler.Sample(numToSample)
	if !ok {
		return nil, ErrUnexpectedSamplerFailure
	}

	nodeIDs := make([]ids.NodeID, numToSample)
	for i, index := range indices {
		nodeIDs[i] = validators[index].id
	}
	return nodeIDs, nil
}

func (w *windower) Delay(ctx context.Context, blockHeight, pChainHeight uint64, validatorID ids.NodeID, maxWindows int) (time.Duration, error) {
	if validatorID == ids.EmptyNodeID {
		return time.Duration(maxWindows) * WindowDuration, nil
	}

	proposers, err := w.Proposers(ctx, blockHeight, pChainHeight, maxWindows)
	if err != nil {
		return 0, err
	}

	delay := time.Duration(0)
	for _, nodeID := range proposers {
		if nodeID == validatorID {
			return delay, nil
		}
		delay += WindowDuration
	}
	return delay, nil
}

func (w *windower) ExpectedProposer(
	ctx context.Context,
	blockHeight,
	pChainHeight,
	slot uint64,
) (ids.NodeID, error) {
	source := prng.NewMT19937_64()
	sampler, validators, err := w.makeSampler(ctx, pChainHeight, source)
	if err != nil {
		return ids.EmptyNodeID, err
	}
	if len(validators) == 0 {
		return ids.EmptyNodeID, ErrAnyoneCanPropose
	}

	return w.expectedProposer(
		validators,
		source,
		sampler,
		blockHeight,
		slot,
	)
}

func (w *windower) MinDelayForProposer(
	ctx context.Context,
	blockHeight,
	pChainHeight uint64,
	nodeID ids.NodeID,
	startSlot uint64,
) (time.Duration, error) {
	source := prng.NewMT19937_64()
	sampler, validators, err := w.makeSampler(ctx, pChainHeight, source)
	if err != nil {
		return 0, err
	}
	if len(validators) == 0 {
		return 0, ErrAnyoneCanPropose
	}

	maxSlot := startSlot + MaxLookAheadSlots
	for slot := startSlot; slot < maxSlot; slot++ {
		expectedNodeID, err := w.expectedProposer(
			validators,
			source,
			sampler,
			blockHeight,
			slot,
		)
		if err != nil {
			return 0, err
		}

		if expectedNodeID == nodeID {
			return time.Duration(slot) * WindowDuration, nil
		}
	}

	// no slots scheduled for the max window we inspect. Return max delay
	return time.Duration(maxSlot) * WindowDuration, nil
}

func (w *windower) makeSampler(
	ctx context.Context,
	pChainHeight uint64,
	source sampler.Source,
) (sampler.WeightedWithoutReplacement, []validatorData, error) {
	// Get the canonical representation of the validator set at the provided
	// p-chain height.
	validatorsMap, err := w.state.GetValidatorSet(ctx, pChainHeight, w.subnetID)
	if err != nil {
		return nil, nil, err
	}

	delete(validatorsMap, ids.EmptyNodeID) // Ignore inactive ACP-77 validators.

	validators := make([]validatorData, 0, len(validatorsMap))
	for k, v := range validatorsMap {
		validators = append(validators, validatorData{
			id:     k,
			weight: v.Weight,
		})
	}

	// Note: validators are sorted by ID. Sorting by weight would not create a
	// canonically sorted list.
	utils.Sort(validators)

	weights := make([]uint64, len(validators))
	for i, validator := range validators {
		weights[i] = validator.weight
	}

	sampler := sampler.NewDeterministicWeightedWithoutReplacement(source)
	return sampler, validators, sampler.Initialize(weights)
}

func (w *windower) expectedProposer(
	validators []validatorData,
	source *prng.MT19937_64,
	sampler sampler.WeightedWithoutReplacement,
	blockHeight,
	slot uint64,
) (ids.NodeID, error) {
	// Slot is reversed to utilize a different state space in the seed than the
	// height. If the slot was not reversed the state space would collide;
	// biasing the seed generation. For example, without reversing the slot
	// height=0 and slot=1 would equal height=1 and slot=0.
	source.Seed(w.chainSource ^ blockHeight ^ bits.Reverse64(slot))
	indices, ok := sampler.Sample(1)
	if !ok {
		return ids.EmptyNodeID, ErrUnexpectedSamplerFailure
	}
	return validators[indices[0]].id, nil
}

func TimeToSlot(start, now time.Time) uint64 {
	if now.Before(start) {
		return 0
	}
	return uint64(now.Sub(start) / WindowDuration)
}
