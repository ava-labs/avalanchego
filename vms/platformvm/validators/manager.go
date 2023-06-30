// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	validatorSetsCacheSize        = 64
	maxRecentlyAcceptedWindowSize = 64
	minRecentlyAcceptedWindowSize = 16
	recentlyAcceptedWindowTTL     = 2 * time.Minute
)

var (
	_ validators.State = (*manager)(nil)

	ErrMissingValidator    = errors.New("missing validator")
	ErrMissingValidatorSet = errors.New("missing validator set")
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
	log logging.Logger,
	cfg config.Config,
	state state.State,
	metrics metrics.Metrics,
	clk *mockable.Clock,
) Manager {
	return &manager{
		log:     log,
		cfg:     cfg,
		state:   state,
		metrics: metrics,
		clk:     clk,
		caches:  make(map[ids.ID]cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput]),
		recentlyAccepted: window.New[ids.ID](
			window.Config{
				Clock:   clk,
				MaxSize: maxRecentlyAcceptedWindowSize,
				MinSize: minRecentlyAcceptedWindowSize,
				TTL:     recentlyAcceptedWindowTTL,
			},
		),
	}
}

type manager struct {
	log     logging.Logger
	cfg     config.Config
	state   state.State
	metrics metrics.Metrics
	clk     *mockable.Clock

	// Maps caches for each subnet that is currently tracked.
	// Key: Subnet ID
	// Value: cache mapping height -> validator set map
	caches map[ids.ID]cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput]

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

func (m *manager) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	lastAcceptedHeight, err := m.GetCurrentHeight(ctx)
	if err != nil {
		return nil, err
	}
	if lastAcceptedHeight < height {
		return nil, database.ErrNotFound
	}

	return m.getValidatorSetFrom(lastAcceptedHeight, height, subnetID)
}

// getValidatorSetFrom fetches the validator set of [subnetID] at [targetHeight]
// or builds it starting from [currentHeight].
//
// Invariant: [m.cfg.Validators] contains the validator set at [currentHeight].
func (m *manager) getValidatorSetFrom(
	currentHeight uint64,
	targetHeight uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	validatorSetsCache, exists := m.caches[subnetID]
	if !exists {
		validatorSetsCache = &cache.LRU[uint64, map[ids.NodeID]*validators.GetValidatorOutput]{Size: validatorSetsCacheSize}
		// Only cache tracked subnets
		if subnetID == constants.PrimaryNetworkID || m.cfg.TrackedSubnets.Contains(subnetID) {
			m.caches[subnetID] = validatorSetsCache
		}
	}

	if validatorSet, ok := validatorSetsCache.Get(targetHeight); ok {
		m.metrics.IncValidatorSetsCached()
		return validatorSet, nil
	}

	// get the start time to track metrics
	startTime := m.clk.Time()

	var (
		validatorSet map[ids.NodeID]*validators.GetValidatorOutput
		err          error
	)
	if subnetID == constants.PrimaryNetworkID {
		validatorSet, err = m.makePrimaryNetworkValidatorSet(currentHeight, targetHeight)
	} else {
		validatorSet, err = m.makeSubnetValidatorSet(currentHeight, targetHeight, subnetID)
	}
	if err != nil {
		return nil, err
	}

	// cache the validator set
	validatorSetsCache.Put(targetHeight, validatorSet)

	endTime := m.clk.Time()
	m.metrics.IncValidatorSetsCreated()
	m.metrics.AddValidatorSetsDuration(endTime.Sub(startTime))
	m.metrics.AddValidatorSetsHeightDiff(currentHeight - targetHeight)
	return validatorSet, nil
}

func (m *manager) makePrimaryNetworkValidatorSet(
	currentHeight uint64,
	targetHeight uint64,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	currentValidators, ok := m.cfg.Validators.Get(constants.PrimaryNetworkID)
	if !ok {
		// This should never happen
		m.log.Error(ErrMissingValidatorSet.Error(),
			zap.Stringer("subnetID", constants.PrimaryNetworkID),
		)
		return nil, ErrMissingValidatorSet
	}
	currentValidatorList := currentValidators.List()

	// Node ID --> Validator information for the node validating the Primary
	// Network.
	validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, len(currentValidatorList))
	for _, vdr := range currentValidatorList {
		validatorSet[vdr.NodeID] = &validators.GetValidatorOutput{
			NodeID:    vdr.NodeID,
			PublicKey: vdr.PublicKey,
			Weight:    vdr.Weight,
		}
	}

	// Rebuild primary network validators at [targetHeight]
	//
	// Note: Since we are attempting to generate the validator set at
	// [targetHeight], we want to apply the diffs from
	// (targetHeight, currentHeight]. Because the state interface is implemented
	// to be inclusive, we apply diffs in [targetHeight + 1, currentHeight].
	lastDiffHeight := targetHeight + 1
	err := m.state.ApplyValidatorWeightDiffs(
		validatorSet,
		currentHeight,
		lastDiffHeight,
		constants.PlatformChainID,
	)
	if err != nil {
		return nil, err
	}

	err = m.state.ApplyValidatorPublicKeyDiffs(
		validatorSet,
		currentHeight,
		lastDiffHeight,
	)
	return validatorSet, err
}

func (m *manager) makeSubnetValidatorSet(
	currentHeight uint64,
	targetHeight uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	currentValidators, ok := m.cfg.Validators.Get(subnetID)
	if !ok {
		currentValidators = validators.NewSet()
		if err := m.state.ValidatorSet(subnetID, currentValidators); err != nil {
			return nil, err
		}
	}
	currentValidatorList := currentValidators.List()

	// Node ID --> Validator information for the node validating the Subnet.
	subnetValidatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, len(currentValidatorList))
	for _, vdr := range currentValidatorList {
		subnetValidatorSet[vdr.NodeID] = &validators.GetValidatorOutput{
			NodeID: vdr.NodeID,
			// PublicKey will be picked from primary validators after weights
			// have been applied
			Weight: vdr.Weight,
		}
	}

	// Rebuild subnet validators at [targetHeight]
	//
	// Note: Since we are attempting to generate the validator set at
	// [targetHeight], we want to apply the diffs from
	// (targetHeight, currentHeight]. Because the state interface is implemented
	// to be inclusive, we apply diffs in [targetHeight + 1, currentHeight].
	lastDiffHeight := targetHeight + 1
	err := m.state.ApplyValidatorWeightDiffs(
		subnetValidatorSet,
		currentHeight,
		lastDiffHeight,
		subnetID,
	)
	if err != nil {
		return nil, err
	}

	currentPrimaryValidators, ok := m.cfg.Validators.Get(constants.PrimaryNetworkID)
	if !ok {
		// This should never happen
		m.log.Error(ErrMissingValidatorSet.Error(),
			zap.Stringer("subnetID", constants.PrimaryNetworkID),
		)
		return nil, ErrMissingValidatorSet
	}

	for nodeID, vdr := range subnetValidatorSet {
		primaryVdr, ok := currentPrimaryValidators.Get(nodeID)
		if !ok {
			// This should never happen
			m.log.Error(ErrMissingValidator.Error(),
				zap.Stringer("subnetID", subnetID),
				zap.Stringer("nodeID", vdr.NodeID),
			)
			return nil, ErrMissingValidator
		}

		vdr.PublicKey = primaryVdr.PublicKey
	}

	err = m.state.ApplyValidatorPublicKeyDiffs(
		subnetValidatorSet,
		currentHeight,
		lastDiffHeight,
	)
	return subnetValidatorSet, err
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
