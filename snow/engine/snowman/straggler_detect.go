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
	minStragglerCheckInterval   = time.Second
	stragglerSuspicionThreshold = 0.75
	lastAcceptedKnownThreshold  = 0.8
	lastAcceptedAheadThreshold  = 0.8
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
	// excluding nodes that have the same last accepted block as we do, along with true if they
	// reach a certain threshold of the total stake.
	getSnapshot func() (snapshot, bool, error)

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
		snapshot, isBehind, err := sd.getSnapshot()
		if err != nil {
			sd.log.Error("Failed to obtain node snapshot", zap.Error(err))
		}
		if !isBehind {
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
	weightProcessing, err := s.lastAccepted.filter(sd.processing).totalWeight()
	if err != nil {
		sd.log.Error("Cumulative weight overflow", zap.Error(err))
		return false
	}

	sd.log.Trace("Counted total weight that accepted blocks we're still processing", zap.Uint64("weight", weightProcessing))

	ratio := float64(weightProcessing) / float64(s.totalValidatorWeight)
	if ratio > stragglerSuspicionThreshold {
		sd.log.Trace("We are straggling behind", zap.Float64("ratio", ratio))
		return true
	}

	sd.log.Trace("Nodes ahead of us:", zap.Float64("ratio", ratio))
	return false
}

func (sd *stragglerDetector) isStraggling(weightTotal, weightLastAcceptedKnown, weightLastAcceptedAhead uint64) bool {
	connectedPercent := sd.connectedPercent()
	if connectedPercent < sd.minConfirmationThreshold {
		// We don't know for sure whether we're behind or not.
		// Even if we're behind, it's pointless to act before we have established
		// connectivity with enough validators.
		sd.log.Verbo("not enough connected stake to determine network info", zap.Float64("ratio", connectedPercent))
		return false
	}

	if weightTotal == 0 {
		sd.log.Trace("Connected to zero weight")
		return false
	}

	// Ensure we have collected last accepted blocks for lastAcceptedKnownThreshold of the total stake.
	if ratio := float64(weightLastAcceptedKnown) / float64(weightTotal); ratio < lastAcceptedKnownThreshold {
		sd.log.Trace("Not collected enough information about last accepted blocks for the validators we are connected to",
			zap.Float64("ratio", ratio))
		return false
	}

	if ratio := float64(weightLastAcceptedAhead) / float64(weightTotal); ratio < lastAcceptedAheadThreshold {
		sd.log.Trace("Most stake we're connected to has the same height as we do",
			zap.Float64("ratio", ratio))
		return false
	}

	return true
}

// getNetworkSnapshot returns a snapshot of the network's nodes and their last accepted blocks,
// and whether we have failed catching up with the network.
func (sd *stragglerDetector) getNetworkSnapshot() (snapshot, bool, error) {
	validators := nodeWeights(sd.connectedValidators().List())
	weightTotal, err := validators.totalWeight()
	if err != nil {
		return snapshot{}, false, err
	}

	nodeWeightToLastAccepted := make(nodeWeightsToBlocks, len(validators))
	for _, vdr := range validators {
		lastAccepted, ok := sd.lastAcceptedByNodeID(vdr.Node)
		if !ok {
			continue
		}
		nodeWeightToLastAccepted[vdr] = lastAccepted
	}

	weightLastAcceptedKnown, err := nodeWeightToLastAccepted.totalWeight()
	if err != nil {
		return snapshot{}, false, err
	}

	// Remove nodes that have the same last accepted block as we do to reach
	// the total weight of validators that have a different last accepted block.
	lastAccepted := sd.lastAccepted()
	nodeWeightToLastAccepted = nodeWeightToLastAccepted.filter(func(id ids.ID) bool {
		return lastAccepted != id
	})
	weightLastAcceptedAhead, err := nodeWeightToLastAccepted.totalWeight()
	if err != nil {
		return snapshot{}, false, err
	}
	sd.log.Trace("Excluding nodes with our own height", zap.Uint64("prev", weightLastAcceptedKnown), zap.Uint64("new", weightLastAcceptedAhead))

	snap := snapshot{
		totalValidatorWeight: weightTotal,
		lastAccepted:         nodeWeightToLastAccepted,
	}
	isStraggling := sd.isStraggling(weightTotal, weightLastAcceptedKnown, weightLastAcceptedAhead)
	if !isStraggling || !sd.haveWeFailedCatchingUp(snap) {
		return snapshot{}, false, nil
	}
	return snap, true, nil
}

type snapshot struct {
	totalValidatorWeight uint64
	lastAccepted         nodeWeightsToBlocks
}

func (s snapshot) isEmpty() bool {
	return s.totalValidatorWeight == 0 || len(s.lastAccepted) == 0
}

type nodeWeightsToBlocks map[ids.NodeWeight]ids.ID

func (nwb nodeWeightsToBlocks) totalWeight() (uint64, error) {
	return nodeWeights(maps.Keys(nwb)).totalWeight()
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
