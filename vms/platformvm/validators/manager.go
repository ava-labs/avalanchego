// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	// MaxRecentlyAcceptedWindowSize is the maximum number of blocks that the
	// recommended minimum height will lag behind the last accepted block.
	MaxRecentlyAcceptedWindowSize = 64
	// MinRecentlyAcceptedWindowSize is the minimum number of blocks that the
	// recommended minimum height will lag behind the last accepted block.
	MinRecentlyAcceptedWindowSize = 0
	// RecentlyAcceptedWindowTTL is the amount of time after a block is accepted
	// to avoid recommending it as the minimum height. The size constraints take
	// precedence over this time constraint.
	RecentlyAcceptedWindowTTL = 30 * time.Second

	validatorSetsCacheSize = 64
)

var (
	_ validators.State = (*manager)(nil)

	errUnfinalizedHeight = errors.New("failed to fetch validator set at unfinalized height")
)

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

	ApplyAllValidatorWeightDiffs(
		ctx context.Context,
		validators map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
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
		subnetID ids.ID,
	) error

	ApplyAllValidatorPublicKeyDiffs(
		ctx context.Context,
		validators map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
	) error

	GetCurrentValidators(ctx context.Context, subnetID ids.ID) ([]*state.Staker, []state.L1Validator, uint64, error)
}

func NewManager(
	cfg config.Internal,
	state State,
	metrics metrics.Metrics,
	clk *mockable.Clock,
) Manager {
	return &manager{
		cfg:     cfg,
		state:   state,
		metrics: metrics,
		clk:     clk,
		cache:   lru.NewCache[uint64, map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput](validatorSetsCacheSize),
		caches:  make(map[ids.ID]cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput]),
		recentlyAccepted: window.New[ids.ID](
			window.Config{
				Clock:   clk,
				MaxSize: MaxRecentlyAcceptedWindowSize,
				MinSize: MinRecentlyAcceptedWindowSize,
				TTL:     RecentlyAcceptedWindowTTL,
			},
		),
	}
}

// TODO: Remove requirement for the P-chain's context lock to be held when
// calling exported functions.
type manager struct {
	cfg     config.Internal
	state   State
	metrics metrics.Metrics
	clk     *mockable.Clock

	// Caches all validator sets at a given height.
	cache cache.Cacher[uint64, map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput]

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
		return maps.Clone(validatorSet), nil
	}

	// get the start time to track metrics
	startTime := m.clk.Time()

	validatorSet, currentHeight, err := m.makeValidatorSet(ctx, targetHeight, subnetID)
	if err != nil {
		return nil, err
	}

	// cache the validator set
	validatorSetsCache.Put(targetHeight, validatorSet)

	duration := m.clk.Time().Sub(startTime)
	m.metrics.IncValidatorSetsCreated()
	m.metrics.AddValidatorSetsDuration(duration)
	m.metrics.AddValidatorSetsHeightDiff(currentHeight - targetHeight)
	return maps.Clone(validatorSet), nil
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

	validatorSetsCache = lru.NewCache[uint64, map[ids.NodeID]*validators.GetValidatorOutput](validatorSetsCacheSize)
	m.caches[subnetID] = validatorSetsCache
	return validatorSetsCache
}

// TODO this can fail if we query a targetHeight before the new indexes existed
func (m *manager) makeAllValidatorSets(
	ctx context.Context,
	targetHeight uint64,
) (map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	allValidators, currentHeight, err := m.getAllCurrentValidatorSets(ctx)
	if err != nil {
		return nil, 0, err
	}
	if currentHeight < targetHeight {
		return nil, 0, fmt.Errorf("%w: current P-chain height (%d) < requested P-Chain height (%d)",
			errUnfinalizedHeight,
			currentHeight,
			targetHeight,
		)
	}

	// Rebuild subnet validators at [targetHeight]
	//
	// Note: Since we are attempting to generate the validator set at
	// [targetHeight], we want to apply the diffs from
	// (targetHeight, currentHeight]. Because the state interface is implemented
	// to be inclusive, we apply diffs in [targetHeight + 1, currentHeight].
	lastDiffHeight := targetHeight + 1
	err = m.state.ApplyAllValidatorWeightDiffs(
		ctx,
		allValidators,
		currentHeight,
		lastDiffHeight,
	)
	if err != nil {
		return nil, 0, err
	}

	err = m.state.ApplyAllValidatorPublicKeyDiffs(
		ctx,
		allValidators,
		currentHeight,
		lastDiffHeight,
	)
	return allValidators, currentHeight, err
}

func (m *manager) makeValidatorSet(
	ctx context.Context,
	targetHeight uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	validatorSet, currentHeight, err := m.getCurrentValidatorSet(ctx, subnetID)
	if err != nil {
		return nil, 0, err
	}
	if currentHeight < targetHeight {
		return nil, 0, fmt.Errorf("%w with SubnetID = %s: current P-chain height (%d) < requested P-Chain height (%d)",
			errUnfinalizedHeight,
			subnetID,
			currentHeight,
			targetHeight,
		)
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
		validatorSet,
		currentHeight,
		lastDiffHeight,
		subnetID,
	)
	if err != nil {
		return nil, 0, err
	}

	err = m.state.ApplyValidatorPublicKeyDiffs(
		ctx,
		validatorSet,
		currentHeight,
		lastDiffHeight,
		subnetID,
	)
	return validatorSet, currentHeight, err
}

func (m *manager) getAllCurrentValidatorSets(
	ctx context.Context,
) (map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	subnetsMap := m.cfg.Validators.GetAllMaps()
	currentHeight, err := m.getCurrentHeight(ctx)
	return subnetsMap, currentHeight, err
}

func (m *manager) getCurrentValidatorSet(
	ctx context.Context,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	subnetMap := m.cfg.Validators.GetMap(subnetID)
	currentHeight, err := m.getCurrentHeight(ctx)
	return subnetMap, currentHeight, err
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

func (m *manager) GetCurrentValidatorSet(ctx context.Context, subnetID ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	result := make(map[ids.ID]*validators.GetCurrentValidatorOutput)
	baseStakers, l1Validators, height, err := m.state.GetCurrentValidators(ctx, subnetID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get current validators: %w", err)
	}

	for _, validator := range baseStakers {
		result[validator.TxID] = &validators.GetCurrentValidatorOutput{
			ValidationID:  validator.TxID,
			NodeID:        validator.NodeID,
			PublicKey:     validator.PublicKey,
			Weight:        validator.Weight,
			StartTime:     uint64(validator.StartTime.Unix()),
			MinNonce:      0,
			IsActive:      true,
			IsL1Validator: false,
		}
	}

	for _, validator := range l1Validators {
		result[validator.ValidationID] = &validators.GetCurrentValidatorOutput{
			ValidationID:  validator.ValidationID,
			NodeID:        validator.NodeID,
			PublicKey:     bls.PublicKeyFromValidUncompressedBytes(validator.PublicKey),
			Weight:        validator.Weight,
			StartTime:     validator.StartTime,
			IsActive:      validator.IsActive(),
			MinNonce:      validator.MinNonce,
			IsL1Validator: true,
		}
	}
	return result, height, nil
}
