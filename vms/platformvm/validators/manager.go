// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	validatorSetsCacheSize        = 64
	maxRecentlyAcceptedWindowSize = 64
	minRecentlyAcceptedWindowSize = 16
	recentlyAcceptedWindowTTL     = 2 * time.Minute
)

var _ validators.State = (*manager)(nil)

// Manager adds the ability to introduce newly accepted blocks IDs to the State
// interface.
type Manager interface {
	validators.State

	// OnAcceptedBlockID registers the ID of the latest accepted block.
	// It is used to update the [recentlyAccepted] sliding window.
	OnAcceptedBlockID(blkID ids.ID)
}

type State interface {
	GetTx(txID ids.ID) (*txs.Tx, status.Status, error)

	GetLastAccepted() ids.ID
	GetStatelessBlock(blockID ids.ID) (block.Block, error)

	// ApplyValidatorWeightDiffs iterates from [startHeight] towards the genesis
	// block until it has applied all of the diffs up to and including
	// [endHeight]. Applying the diffs modifies [validators].
	//
	// Invariant: If attempting to generate the validator set for
	// [endHeight - 1], [validators] must initially contain the validator
	// weights for [startHeight].
	//
	// Note: Because this function iterates towards the genesis, [startHeight]
	// should normally be greater than or equal to [endHeight].
	ApplyValidatorWeightDiffs(
		ctx context.Context,
		validators map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
		subnetID ids.ID,
	) error

	// ApplyValidatorPublicKeyDiffs iterates from [startHeight] towards the
	// genesis block until it has applied all of the diffs up to and including
	// [endHeight]. Applying the diffs modifies [validators].
	//
	// Invariant: If attempting to generate the validator set for
	// [endHeight - 1], [validators] must initially contain the validator
	// weights for [startHeight].
	//
	// Note: Because this function iterates towards the genesis, [startHeight]
	// should normally be greater than or equal to [endHeight].
	ApplyValidatorPublicKeyDiffs(
		ctx context.Context,
		validators map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
	) error
}

func NewManager(
	log logging.Logger,
	cfg config.Config,
	state State,
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

// TODO: Remove requirement for the P-chain's context lock to be held when
// calling exported functions.
type manager struct {
	log     logging.Logger
	cfg     config.Config
	state   State
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
		return m.getCurrentHeight(ctx)
	}

	oldest, ok := m.recentlyAccepted.Oldest()
	if !ok {
		return m.getCurrentHeight(ctx)
	}

	blk, err := m.state.GetStatelessBlock(oldest)
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

func (m *manager) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return m.getCurrentHeight(ctx)
}

// TODO: Pass the context into the state.
func (m *manager) getCurrentHeight(context.Context) (uint64, error) {
	lastAcceptedID := m.state.GetLastAccepted()
	lastAccepted, err := m.state.GetStatelessBlock(lastAcceptedID)
	if err != nil {
		return 0, err
	}
	return lastAccepted.Height(), nil
}

func (m *manager) GetValidatorSet(
	ctx context.Context,
	targetHeight uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	validatorSetsCache := m.getValidatorSetCache(subnetID)

	if validatorSet, ok := validatorSetsCache.Get(targetHeight); ok {
		m.metrics.IncValidatorSetsCached()
		return validatorSet, nil
	}

	// get the start time to track metrics
	startTime := m.clk.Time()

	var (
		validatorSet  map[ids.NodeID]*validators.GetValidatorOutput
		currentHeight uint64
		err           error
	)
	if subnetID == constants.PrimaryNetworkID {
		validatorSet, currentHeight, err = m.makePrimaryNetworkValidatorSet(ctx, targetHeight)
	} else {
		validatorSet, currentHeight, err = m.makeSubnetValidatorSet(ctx, targetHeight, subnetID)
	}
	if err != nil {
		return nil, err
	}

	// cache the validator set
	validatorSetsCache.Put(targetHeight, validatorSet)

	duration := m.clk.Time().Sub(startTime)
	m.metrics.IncValidatorSetsCreated()
	m.metrics.AddValidatorSetsDuration(duration)
	m.metrics.AddValidatorSetsHeightDiff(currentHeight - targetHeight)
	return validatorSet, nil
}

func (m *manager) getValidatorSetCache(subnetID ids.ID) cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput] {
	// Only cache tracked subnets
	if subnetID != constants.PrimaryNetworkID && !m.cfg.TrackedSubnets.Contains(subnetID) {
		return &cache.Empty[uint64, map[ids.NodeID]*validators.GetValidatorOutput]{}
	}

	validatorSetsCache, exists := m.caches[subnetID]
	if exists {
		return validatorSetsCache
	}

	validatorSetsCache = &cache.LRU[uint64, map[ids.NodeID]*validators.GetValidatorOutput]{
		Size: validatorSetsCacheSize,
	}
	m.caches[subnetID] = validatorSetsCache
	return validatorSetsCache
}

func (m *manager) makePrimaryNetworkValidatorSet(
	ctx context.Context,
	targetHeight uint64,
) (map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	validatorSet, currentHeight, err := m.getCurrentPrimaryValidatorSet(ctx)
	if err != nil {
		return nil, 0, err
	}
	if currentHeight < targetHeight {
		return nil, 0, database.ErrNotFound
	}

	// Rebuild primary network validators at [targetHeight]
	//
	// Note: Since we are attempting to generate the validator set at
	// [targetHeight], we want to apply the diffs from
	// (targetHeight, currentHeight]. Because the state interface is implemented
	// to be inclusive, we apply diffs in [targetHeight + 1, currentHeight].
	lastDiffHeight := targetHeight + 1
	err = m.state.ApplyValidatorWeightDiffs(
		ctx,
		validatorSet,
		currentHeight,
		lastDiffHeight,
		constants.PlatformChainID,
	)
	if err != nil {
		return nil, 0, err
	}

	err = m.state.ApplyValidatorPublicKeyDiffs(
		ctx,
		validatorSet,
		currentHeight,
		lastDiffHeight,
	)
	return validatorSet, currentHeight, err
}

func (m *manager) getCurrentPrimaryValidatorSet(
	ctx context.Context,
) (map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	primaryMap := m.cfg.Validators.GetMap(constants.PrimaryNetworkID)
	currentHeight, err := m.getCurrentHeight(ctx)
	return primaryMap, currentHeight, err
}

func (m *manager) makeSubnetValidatorSet(
	ctx context.Context,
	targetHeight uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	subnetValidatorSet, primaryValidatorSet, currentHeight, err := m.getCurrentValidatorSets(ctx, subnetID)
	if err != nil {
		return nil, 0, err
	}
	if currentHeight < targetHeight {
		return nil, 0, database.ErrNotFound
	}

	// Rebuild subnet validators at [targetHeight]
	//
	// Note: Since we are attempting to generate the validator set at
	// [targetHeight], we want to apply the diffs from
	// (targetHeight, currentHeight]. Because the state interface is implemented
	// to be inclusive, we apply diffs in [targetHeight + 1, currentHeight].
	lastDiffHeight := targetHeight + 1
	err = m.state.ApplyValidatorWeightDiffs(
		ctx,
		subnetValidatorSet,
		currentHeight,
		lastDiffHeight,
		subnetID,
	)
	if err != nil {
		return nil, 0, err
	}

	// Update the subnet validator set to include the public keys at
	// [currentHeight]. When we apply the public key diffs, we will convert
	// these keys to represent the public keys at [targetHeight]. If the subnet
	// validator is not currently a primary network validator, it doesn't have a
	// key at [currentHeight].
	for nodeID, vdr := range subnetValidatorSet {
		if primaryVdr, ok := primaryValidatorSet[nodeID]; ok {
			vdr.PublicKey = primaryVdr.PublicKey
		} else {
			vdr.PublicKey = nil
		}
	}

	err = m.state.ApplyValidatorPublicKeyDiffs(
		ctx,
		subnetValidatorSet,
		currentHeight,
		lastDiffHeight,
	)
	return subnetValidatorSet, currentHeight, err
}

func (m *manager) getCurrentValidatorSets(
	ctx context.Context,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	subnetMap := m.cfg.Validators.GetMap(subnetID)
	primaryMap := m.cfg.Validators.GetMap(constants.PrimaryNetworkID)
	currentHeight, err := m.getCurrentHeight(ctx)
	return subnetMap, primaryMap, currentHeight, err
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
