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
	minStragglerCheckInterval                   = time.Second
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

	// minConfirmationThreshold is the minimum stake percentage that below it, we do not check if we are stragglers.
	minConfirmationThreshold float64

	// connectedPercent returns the stake percentage of connected nodes.
	connectedPercent func() float64

	// connectedValidators returns a set of tuples of NodeID and corresponding weight.
	connectedValidators func() set.Set[ids.NodeWeight]

	// lastAcceptedByNodeID returns the last accepted height a node has reported, or false if it is unknown.
	lastAcceptedByNodeID func(id ids.NodeID) (ids.ID, bool)

	// processing returns whether this block ID is known and its descendants have not yet been accepted by consensus.
	// This means that when the last accepted block is given as input, true is returned, as by definition
	// its descendants have not been accepted by consensus, but this block is known.
	// For any block ID belonging to an ancestor of the last accepted block, false is returned,
	// as the last accepted block has been accepted by consensus.
	processing func(id ids.ID) bool

	// lastAccepted returns the last accepted block of this node.
	lastAccepted func() ids.ID

	// getSnapshot returns a snapshot of the network's nodes and their last accepted blocks,
	// or false if it fails from some reason.
	getSnapshot func() (snapshot, bool)

	// haveWeFailedCatchingUp returns whether we have not replicated enough blocks of the given snapshot
	haveWeFailedCatchingUp func(snapshot) bool
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

func newStragglerDetector(
	getTime func() time.Time,
	log logging.Logger,
	minConfirmationThreshold float64,
	lastAccepted func() (ids.ID, uint64),
	connectedValidators func() set.Set[ids.NodeWeight],
	connectedPercent func() float64,
	processing func(id ids.ID) bool,
	lastAcceptedByNodeID func(ids.NodeID) (ids.ID, bool),
) *stragglerDetector {
	sd := &stragglerDetector{
		stragglerDetectorConfig: stragglerDetectorConfig{
			lastAccepted:              dropHeight(lastAccepted),
			processing:                processing,
			minStragglerCheckInterval: minStragglerCheckInterval,
			log:                       log,
			connectedValidators:       connectedValidators,
			connectedPercent:          connectedPercent,
			minConfirmationThreshold:  minConfirmationThreshold,
			lastAcceptedByNodeID:      lastAcceptedByNodeID,
			getTime:                   getTime,
		},
	}

	sd.getSnapshot = sd.getNetworkSnapshot
	sd.haveWeFailedCatchingUp = sd.failedCatchingUp

	return sd
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
		snapshot, ok := sd.getSnapshot()
		if !ok {
			sd.log.Trace("No node snapshot obtained")
			sd.continuousStragglingPeriod = 0
		}
		sd.prevSnapshot = snapshot
	} else {
		if sd.haveWeFailedCatchingUp(sd.prevSnapshot) {
			timeSinceLastCheck := now.Sub(sd.previousStragglerCheckTime)
			sd.continuousStragglingPeriod += timeSinceLastCheck
		} else {
			sd.continuousStragglingPeriod = 0
		}
		sd.prevSnapshot = snapshot{}
	}

	return sd.continuousStragglingPeriod
}

func (sd *stragglerDetector) failedCatchingUp(s snapshot) bool {
	totalValidatorWeight, nodeWeightsToBlocks := s.totalValidatorWeight, s.nodeWeightsToBlocks

	var processingWeight uint64
	for nw, lastAccepted := range nodeWeightsToBlocks {
		if sd.processing(lastAccepted) {
			newProcessingWeight, err := safemath.Add(processingWeight, nw.Weight)
			if err != nil {
				sd.log.Error("Cumulative weight overflow", zap.Uint64("cumulative", processingWeight), zap.Uint64("added", nw.Weight))
				return false
			}
			processingWeight = newProcessingWeight
		}
	}

	sd.log.Trace("Counted total weight that accepted blocks we're still processing", zap.Uint64("weight", processingWeight))

	ratio := float64(processingWeight) / float64(totalValidatorWeight)

	if ratio > stakeThresholdForStragglerSuspicion {
		sd.log.Trace("We are straggling behind", zap.Float64("ratio", ratio))
		return true
	}

	sd.log.Trace("Nodes ahead of us:", zap.Float64("ratio", ratio))

	return false
}

func (sd *stragglerDetector) getNetworkSnapshot() (snapshot, bool) {
	nodeWeightToLastAccepted, totalValidatorWeight := sd.getNetworkInfo()
	if len(nodeWeightToLastAccepted) == 0 {
		return snapshot{}, false
	}

	ourLastAcceptedBlock := sd.lastAccepted()

	prevLastAcceptedCount := len(nodeWeightToLastAccepted)
	for k, v := range nodeWeightToLastAccepted {
		if ourLastAcceptedBlock.Compare(v) == 0 {
			delete(nodeWeightToLastAccepted, k)
		}
	}
	newLastAcceptedCount := len(nodeWeightToLastAccepted)

	sd.log.Trace("Excluding nodes with our own height", zap.Int("prev", prevLastAcceptedCount), zap.Int("new", newLastAcceptedCount))

	// Ensure we have collected last accepted blocks that are not our own last accepted block
	// for at least 80% stake of the total weight we are connected to.

	totalWeightWeKnowItsLastAcceptedBlock, err := nodeWeightToLastAccepted.totalWeight()
	if err != nil {
		sd.log.Error("Failed computing total weight", zap.Error(err))
		return snapshot{}, false
	}

	ratio := float64(totalWeightWeKnowItsLastAcceptedBlock) / float64(totalValidatorWeight)

	if ratio < knownStakeThresholdRequiredForAnalysis {
		sd.log.Trace("Most stake we're connected to has the same height as we do",
			zap.Float64("ratio", ratio))
		return snapshot{}, false
	}

	snap := snapshot{
		nodeWeightsToBlocks:  nodeWeightToLastAccepted,
		totalValidatorWeight: totalValidatorWeight,
	}

	if sd.haveWeFailedCatchingUp(snap) {
		return snap, true
	}

	return snapshot{}, false
}

func (sd *stragglerDetector) getNetworkInfo() (nodeWeightsToBlocks, uint64) {
	ratio := sd.connectedPercent()
	if ratio < sd.minConfirmationThreshold {
		// We don't know for sure whether we're behind or not.
		// Even if we're behind, it's pointless to act before we have established
		// connectivity with enough validators.
		sd.log.Verbo("not enough connected stake to determine network info", zap.Float64("ratio", ratio))
		return nil, 0
	}

	validators := nodeWeights(sd.connectedValidators().List())

	nodeWeightTolastAccepted := make(nodeWeightsToBlocks, len(validators))

	for _, vdr := range validators {
		lastAccepted, ok := sd.lastAcceptedByNodeID(vdr.Node)
		if !ok {
			continue
		}
		nodeWeightTolastAccepted[vdr] = lastAccepted
	}

	totalValidatorWeight, err := validators.totalWeight()
	if err != nil {
		sd.log.Error("Failed computing total weight", zap.Error(err))
		return nil, 0
	}

	totalWeightWeKnowItsLastAcceptedBlock, err := nodeWeightTolastAccepted.totalWeight()
	if err != nil {
		sd.log.Error("Failed computing total weight", zap.Error(err))
		return nil, 0
	}

	if totalValidatorWeight == 0 {
		sd.log.Trace("Connected to zero weight")
		return nil, 0
	}

	ratio = float64(totalWeightWeKnowItsLastAcceptedBlock) / float64(totalValidatorWeight)

	// Ensure we have collected last accepted blocks for at least 80% (or so) stake of the total weight we are connected to.
	if ratio < minimumStakeThresholdRequiredForNetworkInfo {
		sd.log.Trace("Not collected enough information about last accepted blocks for the validators we are connected to",
			zap.Float64("ratio", ratio))
		return nil, 0
	}
	return nodeWeightTolastAccepted, totalValidatorWeight
}

type snapshot struct {
	totalValidatorWeight uint64
	nodeWeightsToBlocks  nodeWeightsToBlocks
}

func (s snapshot) isEmpty() bool {
	return s.totalValidatorWeight == 0 || len(s.nodeWeightsToBlocks) == 0
}

type nodeWeightsToBlocks map[ids.NodeWeight]ids.ID

func (nwb nodeWeightsToBlocks) totalWeight() (uint64, error) {
	return nodeWeights(maps.Keys(nwb)).totalWeight()
}

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
