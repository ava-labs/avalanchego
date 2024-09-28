// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	minStragglerCheckInterval                   = 10 * time.Second
	stakeThresholdForStragglerSuspicion         = 0.75
	minimumStakeThresholdRequiredForNetworkInfo = 0.8
	knownStakeThresholdRequiredForAnalysis      = 0.8
)

type stragglerDetectorConfig struct {
	// getTime returns the current time
	getTime func() time.Time

	// minStragglerCheckInterval determines how frequently we are allowed to check if we are stragglers.
	minStragglerCheckInterval time.Duration

	// log logs events
	log logging.Logger

	// getSnapshot returns a snapshot of the network's nodes and their last accepted blocks,
	// excluding nodes that have the same last accepted block as we do.
	// Returns false if it fails from some reason.
	getSnapshot func() (snapshot, bool)

	// areWeBehindTheRest returns whether we have not replicated enough blocks of the given snapshot
	areWeBehindTheRest func(snapshot) bool
}

type stragglerDetector struct {
	stragglerDetectorConfig

	// continuousStragglingPeriod defines the time we have been straggling continuously.
	continuousStragglingPeriod time.Duration

	// previousStragglerCheckTime is the last time we checked whether
	// our block height is behind the rest of the network
	previousStragglerCheckTime time.Time

	// prevSnapshot is the snapshot from a past iteration.
	prevSnapshot snapshot
}

func newStragglerDetector(config stragglerDetectorConfig) *stragglerDetector {
	return &stragglerDetector{
		stragglerDetectorConfig: config,
	}
}

// CheckIfWeAreStragglingBehind returns for how long our ledger is behind the rest
// of the nodes in the network. If we are not behind, zero is returned.
func (sd *stragglerDetector) CheckIfWeAreStragglingBehind() time.Duration {
	now := sd.getTime()
	if sd.previousStragglerCheckTime.IsZero() {
		sd.previousStragglerCheckTime = now
		return 0
	}

	// Don't check too often, only once in every minStragglerCheckInterval
	if sd.previousStragglerCheckTime.Add(sd.minStragglerCheckInterval).After(now) {
		return 0
	}

	defer func() {
		sd.previousStragglerCheckTime = now
	}()

	if sd.prevSnapshot.isEmpty() {
		sd.obtainSnapshot()
	} else {
		sd.evaluateSnapshot(now)
	}

	return sd.continuousStragglingPeriod
}

func (sd *stragglerDetector) obtainSnapshot() {
	snap, ok := sd.getSnapshot()
	sd.prevSnapshot = snap
	if !ok || !sd.areWeBehindTheRest(snap) {
		sd.log.Trace("No node snapshot obtained")
		sd.continuousStragglingPeriod = 0
		sd.prevSnapshot = snapshot{}
	}
}

func (sd *stragglerDetector) evaluateSnapshot(now time.Time) {
	if sd.areWeBehindTheRest(sd.prevSnapshot) {
		timeSinceLastCheck := now.Sub(sd.previousStragglerCheckTime)
		sd.continuousStragglingPeriod += timeSinceLastCheck
	} else {
		sd.continuousStragglingPeriod = 0
	}
	sd.prevSnapshot = snapshot{}
}

type snapshotAnalyzer struct {
	log logging.Logger

	// processing returns whether this block ID is known and its descendants have not yet been accepted by consensus.
	// This means that when the last accepted block is given as input, true is returned, as by definition
	// its descendants have not been accepted by consensus, but this block is known.
	// For any block ID belonging to an ancestor of the last accepted block, false is returned,
	// as the last accepted block has been accepted by consensus.
	processing func(id ids.ID) bool
}

func (sa *snapshotAnalyzer) areWeBehindTheRest(s snapshot) bool {
	if s.isEmpty() {
		return false
	}

	totalValidatorWeight, nodeWeightsToBlocks := s.totalValidatorWeight, s.lastAcceptedBlockID

	processingWeight, err := nodeWeightsToBlocks.filter(sa.processing).totalWeight()
	if err != nil {
		sa.log.Error("Failed computing total weight", zap.Error(err))
		return false
	}

	sa.log.Trace("Counted total weight that accepted blocks we're still processing", zap.Uint64("weight", processingWeight))

	ratio := float64(processingWeight) / float64(totalValidatorWeight)

	if ratio > stakeThresholdForStragglerSuspicion {
		sa.log.Trace("We are straggling behind", zap.Float64("ratio", ratio))
		return true
	}

	sa.log.Trace("Nodes ahead of us:", zap.Float64("ratio", ratio))

	return false
}

type snapshotter struct {
	// log logs events
	log logging.Logger

	// minConfirmationThreshold is the minimum stake percentage that below it, we do not check if we are stragglers.
	minConfirmationThreshold float64

	// lastAccepted returns the last accepted block of this node.
	lastAccepted func() ids.ID

	// connectedValidators returns a set of tuples of NodeID and corresponding weight.
	connectedValidators func() set.Set[ids.NodeWeight]

	// lastAcceptedByNodeID returns the last accepted height a node has reported, or false if it is unknown.
	lastAcceptedByNodeID func(id ids.NodeID) (ids.ID, bool)
}

func (s *snapshotter) validateNetInfo(netInfo netInfo) bool {
	if netInfo.totalValidatorWeight == 0 {
		s.log.Trace("Connected to zero weight")
		return false
	}

	totalKnownLastBlockStakePercent := float64(netInfo.totalWeightWeKnowItsLastAcceptedBlock) / float64(netInfo.totalValidatorWeight)
	stakeAheadOfUs := float64(netInfo.totalPendingStake) / float64(netInfo.totalValidatorWeight)

	// Ensure we have collected last accepted blocks for at least 80% (or so) stake of the total weight we are connected to.
	if totalKnownLastBlockStakePercent < minimumStakeThresholdRequiredForNetworkInfo {
		s.log.Trace("Not collected enough information about last accepted blocks for the validators we are connected to",
			zap.Float64("ratio", totalKnownLastBlockStakePercent))
		return false
	}

	if stakeAheadOfUs < knownStakeThresholdRequiredForAnalysis {
		s.log.Trace("Most stake we're connected to has the same height as we do",
			zap.Float64("ratio", stakeAheadOfUs))
		return false
	}

	return true
}

func (s *snapshotter) getNetworkSnapshot() (snapshot, bool) {
	ourLastAcceptedBlock := s.lastAccepted()

	netInfo, err := s.getNetworkInfo(ourLastAcceptedBlock)
	if err != nil {
		return snapshot{}, false
	}

	if !s.validateNetInfo(netInfo) {
		return snapshot{}, false
	}

	snap := snapshot{
		lastAcceptedBlockID:  netInfo.nodeWeightToLastAccepted,
		totalValidatorWeight: netInfo.totalValidatorWeight,
	}

	return snap, true
}

func (s *snapshotter) getNetworkInfo(ourLastAcceptedBlock ids.ID) (netInfo, error) {
	var res netInfo

	validators := nodeWeights(s.connectedValidators().List())
	nodeWeightToLastAccepted := make(nodeWeightsToBlocks, len(validators))

	for _, vdr := range validators {
		lastAccepted, ok := s.lastAcceptedByNodeID(vdr.ID)
		if !ok {
			continue
		}
		nodeWeightToLastAccepted[vdr] = lastAccepted
	}

	totalValidatorWeight, err := validators.totalWeight()
	if err != nil {
		s.log.Error("Failed computing total weight", zap.Error(err))
		return netInfo{}, err
	}
	res.totalValidatorWeight = totalValidatorWeight

	totalWeightWeKnowItsLastAcceptedBlock, err := nodeWeightToLastAccepted.totalWeight()
	if err != nil {
		s.log.Error("Failed computing total weight", zap.Error(err))
		return netInfo{}, err
	}
	res.totalWeightWeKnowItsLastAcceptedBlock = totalWeightWeKnowItsLastAcceptedBlock

	prevLastAcceptedCount := len(nodeWeightToLastAccepted)

	// Ensure we have collected last accepted blocks that are not our own last accepted block.
	nodeWeightToLastAccepted = nodeWeightToLastAccepted.filter(func(id ids.ID) bool {
		return ourLastAcceptedBlock.Compare(id) != 0
	})

	res.nodeWeightToLastAccepted = nodeWeightToLastAccepted

	totalPendingStake, err := nodeWeightToLastAccepted.totalWeight()
	if err != nil {
		s.log.Error("Failed computing total weight", zap.Error(err))
		return netInfo{}, err
	}

	res.totalPendingStake = totalPendingStake

	s.log.Trace("Excluding nodes with our own height", zap.Int("prev", prevLastAcceptedCount), zap.Uint64("new", totalPendingStake))

	return res, nil
}

type netInfo struct {
	totalPendingStake                     uint64
	totalValidatorWeight                  uint64
	totalWeightWeKnowItsLastAcceptedBlock uint64
	nodeWeightToLastAccepted              nodeWeightsToBlocks
}

type snapshot struct {
	totalValidatorWeight uint64
	lastAcceptedBlockID  nodeWeightsToBlocks
}

func (s snapshot) isEmpty() bool {
	return s.totalValidatorWeight == 0 || len(s.lastAcceptedBlockID) == 0
}

type nodeWeightsToBlocks map[ids.NodeWeight]ids.ID

func (nwb nodeWeightsToBlocks) totalWeight() (uint64, error) {
	return nodeWeights(maps.Keys(nwb)).totalWeight()
}

// dropHeight removes the second return parameter from the function f() and keeps its first return parameter, ids.ID.
func dropHeight(f func() (ids.ID, uint64)) func() ids.ID {
	return func() ids.ID {
		id, _ := f()
		return id
	}
}

type nodeWeights []ids.NodeWeight

func (nws nodeWeights) totalWeight() (uint64, error) {
	var weight uint64
	for _, nw := range nws {
		newWeight, err := safemath.Add(weight, nw.Weight)
		if err != nil {
			return 0, fmt.Errorf("cumulative weight: %d, tried to add %d: %w", weight, nw.Weight, err)
		}
		weight = newWeight
	}
	return weight, nil
}

func (nwb nodeWeightsToBlocks) filter(f func(ids.ID) bool) nodeWeightsToBlocks {
	filtered := make(nodeWeightsToBlocks, len(nwb))
	for nw, id := range nwb {
		if f(id) {
			filtered[nw] = id
		}
	}
	return filtered
}
