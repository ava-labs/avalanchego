// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	validatorSetsCacheSize        = 64
	maxRecentlyAcceptedWindowSize = 256
	recentlyAcceptedWindowTTL     = 5 * time.Minute
)

var (
	_ validators.State = (*manager)(nil)

	ErrMissingValidator = errors.New("missing validator")
)

// Manager adds the ability to introduce newly acceted blocks IDs to the State
// interface.
type Manager interface {
	validators.State

	// OnAcceptedBlockID registers the ID of the latest accepted block.
	// It is used to update the [recentlyAccepted] sliding window.
	OnAcceptedBlockID(blkID ids.ID)
}

func NewManager(
	cfg config.Config,
	state state.State,
	metrics metrics.Metrics,
	clk *mockable.Clock,
) Manager {
	return &manager{
		cfg:     cfg,
		state:   state,
		metrics: metrics,
		clk:     clk,
		caches:  make(map[ids.ID]cache.Cacher[uint64, []*validators.GetValidatorOutput]),
		recentlyAccepted: window.New[ids.ID](
			window.Config{
				Clock:   clk,
				MaxSize: maxRecentlyAcceptedWindowSize,
				TTL:     recentlyAcceptedWindowTTL,
			},
		),
	}
}

type manager struct {
	cfg     config.Config
	state   state.State
	metrics metrics.Metrics
	clk     *mockable.Clock

	// Maps caches for each subnet that is currently tracked.
	// Key: Subnet ID
	// Value: cache mapping height -> validator set map
	caches map[ids.ID]cache.Cacher[uint64, []*validators.GetValidatorOutput]

	// sliding window of blocks that were recently accepted
	recentlyAccepted window.Window[ids.ID]
}

// GetMinimumHeight returns the height of the most recent block beyond the
// horizon of our recentlyAccepted window.
//
// Because the time between blocks is arbitrary, we're only guaranteed that
// the window's configured TTL amount of time has passed once an element
// expires from the window.
//
// To try to always return a block older than the window's TTL, we return the
// parent of the oldest element in the window (as an expired element is always
// guaranteed to be sufficiently stale). If we haven't expired an element yet
// in the case of a process restart, we default to the lastAccepted block's
// height which is likely (but not guaranteed) to also be older than the
// window's configured TTL.
//
// If [UseCurrentHeight] is true, we override the block selection policy
// described above and we will always return the last accepted block height
// as the minimum.
func (m *manager) GetMinimumHeight(ctx context.Context) (uint64, error) {
	if m.cfg.UseCurrentHeight {
		return m.GetCurrentHeight(ctx)
	}

	oldest, ok := m.recentlyAccepted.Oldest()
	if !ok {
		return m.GetCurrentHeight(ctx)
	}

	blk, _, err := m.state.GetStatelessBlock(oldest)
	if err != nil {
		return 0, err
	}

	// We subtract 1 from the height of [oldest] because we want the height of
	// the last block accepted before the [recentlyAccepted] window.
	//
	// There is guaranteed to be a block accepted before this window because the
	// first block added to [recentlyAccepted] window is >= height 1.
	return blk.Height() - 1, nil
}

func (m *manager) GetCurrentHeight(context.Context) (uint64, error) {
	lastAcceptedID := m.state.GetLastAccepted()
	lastAccepted, _, err := m.state.GetStatelessBlock(lastAcceptedID)
	if err != nil {
		return 0, err
	}
	return lastAccepted.Height(), nil
}

func (m *manager) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) ([]*validators.GetValidatorOutput, error) {
	validatorSetsCache, exists := m.caches[subnetID]
	if !exists {
		validatorSetsCache = &cache.LRU[uint64, []*validators.GetValidatorOutput]{Size: validatorSetsCacheSize}
		// Only cache tracked subnets
		if subnetID == constants.PrimaryNetworkID || m.cfg.TrackedSubnets.Contains(subnetID) {
			m.caches[subnetID] = validatorSetsCache
		}
	}

	if validatorSet, ok := validatorSetsCache.Get(height); ok {
		m.metrics.IncValidatorSetsCached()
		return validatorSet, nil
	}

	lastAcceptedHeight, err := m.GetCurrentHeight(ctx)
	if err != nil {
		return nil, err
	}
	if lastAcceptedHeight < height {
		return nil, database.ErrNotFound
	}

	// get the start time to track metrics
	startTime := m.clk.Time()

	currentSubnetValidatorSet, ok := m.cfg.Validators.Get(subnetID)
	if !ok {
		currentSubnetValidatorSet = validators.NewSet()
		if err := m.state.ValidatorSet(subnetID, currentSubnetValidatorSet); err != nil {
			return nil, err
		}
	}
	currentPrimaryNetworkValidators, ok := m.cfg.Validators.Get(constants.PrimaryNetworkID)
	if !ok {
		// This should never happen
		return nil, ErrMissingValidator
	}

	currentSubnetValidatorList := currentSubnetValidatorSet.List()
	currentSubnetValidators := make([]*maskedValidator, 0, len(currentSubnetValidatorList))
	currentSubnetValidatorIndices := make(map[ids.NodeID]int)
	for i, validator := range currentSubnetValidatorList {
		primaryVdr, ok := currentPrimaryNetworkValidators.Get(validator.NodeID)
		if !ok {
			// This should never happen
			return nil, fmt.Errorf("%w: %s", ErrMissingValidator, validator.NodeID)
		}

		currentSubnetValidators = append(currentSubnetValidators, &maskedValidator{
			GetValidatorOutput: &validators.GetValidatorOutput{
				NodeID:    validator.NodeID,
				PublicKey: primaryVdr.PublicKey,
				Weight:    validator.Weight,
			},
			masked: false,
		})
		currentSubnetValidatorIndices[validator.NodeID] = i
	}

	validatorsFromDiffs := make([]*maskedValidator, 0)
	validatorsFromDiffsIndices := make(map[ids.NodeID]int)
	for diffHeight := lastAcceptedHeight; diffHeight > height; diffHeight-- {
		if validatorsFromDiffs, err = m.applyValidatorDiffs(
			currentSubnetValidators,
			currentSubnetValidatorIndices,
			validatorsFromDiffs,
			validatorsFromDiffsIndices,
			subnetID,
			diffHeight,
		); err != nil {
			return nil, err
		}
	}

	// The current validator set is guaranteed to be sorted. Sort the set of
	// validators calculated from applying diffs, so we can merge the two sorted
	// lists.
	sort.Slice(validatorsFromDiffs, func(i, j int) bool {
		return validatorsFromDiffs[i].less(validatorsFromDiffs[j])
	})

	result := merge(currentSubnetValidators, validatorsFromDiffs)

	// cache the validator set
	validatorSetsCache.Put(height, result)

	endTime := m.clk.Time()
	m.metrics.IncValidatorSetsCreated()
	m.metrics.AddValidatorSetsDuration(endTime.Sub(startTime))
	m.metrics.AddValidatorSetsHeightDiff(lastAcceptedHeight - height)
	return result, nil
}

func (m *manager) applyValidatorDiffs(
	currentSubnetValidators []*maskedValidator,
	currentSubnetValidatorIndices map[ids.NodeID]int,
	validatorsFromDiffs []*maskedValidator,
	validatorsFromDiffsIndices map[ids.NodeID]int,
	subnetID ids.ID,
	height uint64,
) ([]*maskedValidator, error) {
	weightDiffs, err := m.state.GetValidatorWeightDiffs(height, subnetID)
	if err != nil {
		return nil, err
	}

	for nodeID, weightDiff := range weightDiffs {
		var (
			vdr      *maskedValidator
			fromDiff bool
		)

		idx, ok := currentSubnetValidatorIndices[nodeID]
		if ok {
			vdr = currentSubnetValidators[idx]
			if vdr.masked {
				vdr.masked = false
			}
		} else {
			fromDiff = true

			idx, ok = validatorsFromDiffsIndices[nodeID]
			if ok {
				vdr = validatorsFromDiffs[idx]
				if vdr.masked {
					vdr.masked = false
				}
			} else {
				vdr = &maskedValidator{
					GetValidatorOutput: &validators.GetValidatorOutput{
						NodeID: nodeID,
					},
				}

				validatorsFromDiffsIndices[vdr.NodeID] = len(validatorsFromDiffs)
				validatorsFromDiffs = append(validatorsFromDiffs, vdr)
			}
		}

		// The weight of this node changed at this block.
		if weightDiff.Decrease {
			// The validator's weight was decreased at this block, so in the
			// prior block it was higher.
			vdr.Weight, err = math.Add64(vdr.Weight, weightDiff.Amount)
		} else {
			// The validator's weight was increased at this block, so in the
			// prior block it was lower.
			vdr.Weight, err = math.Sub(vdr.Weight, weightDiff.Amount)
		}
		if err != nil {
			return nil, err
		}

		if vdr.Weight == 0 {
			// The validator's weight was 0 before this block so
			// they weren't in the validator set.
			if fromDiff {
				if i, ok := validatorsFromDiffsIndices[vdr.NodeID]; ok {
					validatorsFromDiffs[i].masked = true
				}
			} else {
				if i, ok := currentSubnetValidatorIndices[vdr.NodeID]; ok {
					currentSubnetValidators[i].masked = true
				}
			}
		}
	}

	pkDiffs, err := m.state.GetValidatorPublicKeyDiffs(height)
	if err != nil {
		return nil, nil
	}

	for nodeID, pk := range pkDiffs {
		// pkDiffs includes all primary network key diffs, if we are
		// fetching a subnet's validator set, we should ignore non-subnet
		// validators.

		// The validator's public key was removed at this block, so it
		// was in the validator set before.
		if i, ok := currentSubnetValidatorIndices[nodeID]; ok {
			currentSubnetValidators[i].PublicKey = pk
			continue
		}

		if i, ok := validatorsFromDiffsIndices[nodeID]; ok {
			validatorsFromDiffs[i].PublicKey = pk
		}
	}

	return validatorsFromDiffs, nil
}

func merge(currentSubnetValidators,
	validatorsFromDiffs []*maskedValidator,
) []*validators.GetValidatorOutput {
	result := make([]*validators.GetValidatorOutput, 0,
		len(currentSubnetValidators)+len(validatorsFromDiffs))

	var i, j int
	for i < len(currentSubnetValidators) && j < len(validatorsFromDiffs) {
		currentValidator := currentSubnetValidators[i]
		validatorFromDiff := validatorsFromDiffs[j]

		if currentValidator.less(validatorFromDiff) {
			result = append(result, currentValidator.GetValidatorOutput)
			i++
			continue
		}

		result = append(result, validatorFromDiff.GetValidatorOutput)
		j++
	}

	for ; i < len(currentSubnetValidators); i++ {
		vdr := currentSubnetValidators[i]
		if vdr.masked {
			continue
		}
		result = append(result, vdr.GetValidatorOutput)
	}

	for ; j < len(validatorsFromDiffs); j++ {
		vdr := validatorsFromDiffs[j]
		if vdr.masked {
			continue
		}
		result = append(result, vdr.GetValidatorOutput)
	}

	return result
}

func (m *manager) GetSubnetID(_ context.Context, chainID ids.ID) (ids.ID, error) {
	if chainID == constants.PlatformChainID {
		return constants.PrimaryNetworkID, nil
	}

	chainTx, _, err := m.state.GetTx(chainID)
	if err != nil {
		return ids.Empty, fmt.Errorf(
			"problem retrieving blockchain %q: %w",
			chainID,
			err,
		)
	}
	chain, ok := chainTx.Unsigned.(*txs.CreateChainTx)
	if !ok {
		return ids.Empty, fmt.Errorf("%q is not a blockchain", chainID)
	}
	return chain.SubnetID, nil
}

func (m *manager) OnAcceptedBlockID(blkID ids.ID) {
	m.recentlyAccepted.Add(blkID)
}

type maskedValidator struct {
	*validators.GetValidatorOutput
	masked bool
}

func (m *maskedValidator) less(other *maskedValidator) bool {
	return m.NodeID.Less(other.NodeID)
}
