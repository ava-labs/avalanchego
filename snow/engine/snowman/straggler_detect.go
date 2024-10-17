// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"errors"
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

// snapshotAnalyzer analyzes a snapshot and returns true whether
// the caller is behind the majority of the network
type snapshotAnalyzer struct {
	// log is used to log events
	log logging.Logger
	// lastAcceptedHeight returns the last accepted block of this node.
	lastAcceptedHeight func() uint64
}

func (sa *snapshotAnalyzer) areWeBehindTheRest(s snapshot) bool {
	if s.isEmpty() {
		return false
	}

	totalValidatorWeight, nodeWeightsToBlockHeights := s.totalValidatorWeight, s.lastAcceptedBlockHeight

	filter := higherThanGivenHeight(sa.lastAcceptedHeight())
	totalWeightOfNodesAheadOfUs, err := nodeWeightsToBlockHeights.filter(filter).totalWeight()
	if err != nil {
		sa.log.Error("Failed computing total weight", zap.Error(err))
		return false
	}

	sa.log.Trace("Counted total weight of nodes that are ahead of us", zap.Uint64("weight", totalWeightOfNodesAheadOfUs))

	ratio := float64(totalWeightOfNodesAheadOfUs) / float64(totalValidatorWeight)

	if ratio > stakeThresholdForStragglerSuspicion {
		sa.log.Trace("We are straggling behind", zap.Float64("ratio", ratio))
		return true
	}

	sa.log.Trace("Nodes ahead of us:", zap.Float64("ratio", ratio))

	return false
}

func higherThanGivenHeight(givenHeight uint64) func(height uint64) bool {
	return func(height uint64) bool {
		return height > givenHeight
	}
}

type snapshotter struct {
	// log logs events
	log logging.Logger

	// totalWeight returns the total amount of weight.
	totalWeight func() (uint64, error)

	// minConfirmationThreshold is the minimum stake percentage that below it, we do not check if we are stragglers.
	minConfirmationThreshold float64

	// connectedValidators returns a set of tuples of NodeID and corresponding weight.
	connectedValidators func() set.Set[ids.NodeWeight]

	// lastAcceptedHeightByNodeID returns the last accepted height a node has reported, or false if it is unknown.
	lastAcceptedHeightByNodeID func(id ids.NodeID) (ids.ID, uint64, bool)
}

func (s *snapshotter) getNetworkSnapshot() (snapshot, bool) {
	totalValidatorWeight, nodeWeightsToLastAcceptedHeight, err := s.getNetworkInfo()
	if err != nil {
		s.log.Trace("Failed getting network info", zap.Error(err))
		return snapshot{}, false
	}

	snap := snapshot{
		lastAcceptedBlockHeight: nodeWeightsToLastAcceptedHeight,
		totalValidatorWeight:    totalValidatorWeight,
	}

	return snap, true
}

func (s *snapshotter) getNetworkInfo() (uint64, nodeWeightsToHeight, error) {
	totalValidatorWeight, err := s.totalWeight()
	if err != nil {
		return 0, nil, err
	}

	validators := nodeWeights(s.connectedValidators().List())
	nodeWeightToLastAcceptedHeight := make(nodeWeightsToHeight, len(validators))

	for _, vdr := range validators {
		_, lastAcceptedHeight, ok := s.lastAcceptedHeightByNodeID(vdr.ID)
		if !ok {
			continue
		}
		nodeWeightToLastAcceptedHeight[vdr] = lastAcceptedHeight
	}

	totalKnownConnectedWeight, err := nodeWeightToLastAcceptedHeight.totalWeight()
	if err != nil {
		return 0, nil, err
	}

	if totalValidatorWeight == 0 {
		return 0, nil, errors.New("connected to zero weight")
	}

	knownPercentageOfConnectedValidators := 100 * float64(totalKnownConnectedWeight) / float64(totalValidatorWeight)

	if knownPercentageOfConnectedValidators < 100*minimumStakeThresholdRequiredForNetworkInfo {
		s.log.Trace("Not collected enough information about last accepted block heights",
			zap.Int("percentage", int(knownPercentageOfConnectedValidators)))
		return 0, nil, errors.New("did not gather enough statistics")
	}

	return totalValidatorWeight, nodeWeightToLastAcceptedHeight, nil
}

type snapshot struct {
	totalValidatorWeight    uint64
	lastAcceptedBlockHeight nodeWeightsToHeight
}

func (s snapshot) isEmpty() bool {
	return s.totalValidatorWeight == 0 || len(s.lastAcceptedBlockHeight) == 0
}

type nodeWeightsToHeight map[ids.NodeWeight]uint64

func (nwh nodeWeightsToHeight) totalWeight() (uint64, error) {
	return nodeWeights(maps.Keys(nwh)).totalWeight()
}

// onlyHeight removes the first return parameter from the function f() and keeps its second return parameter, the height.
func onlyHeight(f func() (ids.ID, uint64)) func() uint64 {
	return func() uint64 {
		_, height := f()
		return height
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

func (nwh nodeWeightsToHeight) filter(f func(height uint64) bool) nodeWeightsToHeight {
	filtered := make(nodeWeightsToHeight, len(nwh))
	for nw, height := range nwh {
		if f(height) {
			filtered[nw] = height
		}
	}
	return filtered
}
