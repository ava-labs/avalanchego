// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"fmt"
	"slices"
	"strings"
)

var (
	errBlacklistTooManyUpdates              = fmt.Errorf("too many blacklist updates")
	errBlacklistTooManySuspectedNodes       = fmt.Errorf("too many suspected nodes in blacklist")
	errBlacklistInvalidNodeIndex            = fmt.Errorf("invalid node index in blacklist update")
	errBlacklistNodeIndexAlreadyUpdated     = fmt.Errorf("node index already updated")
	errBlacklistNodeAlreadyBlacklisted      = fmt.Errorf("node index already blacklisted")
	errBlacklistNodeNotBlacklisted          = fmt.Errorf("node index not blacklisted, cannot be redeemed")
	errBlacklistInvalidUpdateType           = fmt.Errorf("invalid blacklist update type")
	errBlacklistInvalidMaxBlacklistedRounds = fmt.Errorf("block contains an invalid max blacklisted rounds")
	errBlacklistInvalidUpdates              = fmt.Errorf("block contains invalid blacklist updates")
	errBlacklistInvalidBlacklist            = fmt.Errorf("block contains an invalid blacklist")
)

// GenesisMaxBlacklistedRounds is the max blacklisted rounds a blacklist starts with when the chain has no previous
// max blacklisted orbits to derive it from.
// It is configured to be 3 * 4 * 5 * 7 * 11 because the max blacklisted orbits is computed as rounds / nodeCount,
// so this value is divisible by common validator counts.
const GenesisMaxBlacklistedRounds = 3 * 4 * 5 * 7 * 11

// orbit returns the total number of times the given node has been selected as leader
// from genesis up to and including the specified round.
func orbit(round uint64, nodeIndex uint16, nodeCount uint16) uint64 {
	inCycle := round % uint64(nodeCount)
	cycles := round / uint64(nodeCount)

	if inCycle >= uint64(nodeIndex) {
		return cycles + 1
	}

	return cycles
}

// Blacklist stores the state of the blacklist.
// It can be derived by applying the recorded updates to the parent block's blacklist.
type Blacklist struct {
	// maxBlacklistedRounds is the maximum number of rounds a node can be blacklisted for.
	// Each block builder may move it by at most N (nodeCount) relative to the parent block,
	// towards the value in its local configuration, which allows it to be changed via a rolling upgrade.
	maxBlacklistedRounds uint16 `canoto:"uint,1"`

	// State: Below are fields corresponding to state.
	// They are optional fields and may not be present
	// in a Blacklist.

	// suspectedNodes is the list of nodes that are currently suspected.
	// it's the inner state of the blacklist.
	suspectedNodes suspectedNodes `canoto:"repeated value,2"`

	// updates is the list of modifications that a block builder is proposing
	// to perform to the blacklist.
	updates BlacklistUpdates `canoto:"repeated value,3"`

	canotoData canotoData_Blacklist
}

func NewBlacklist(maxBlacklistedRounds uint16) Blacklist {
	return Blacklist{
		maxBlacklistedRounds: maxBlacklistedRounds,
	}
}

// MaxBlacklistedRounds returns the maximum number of rounds a node can be blacklisted for.
func (bl *Blacklist) MaxBlacklistedRounds() uint16 {
	return bl.maxBlacklistedRounds
}

// Stateless returns a new blacklist with the same configuration as the current blacklist, but without any state.
// It derives the maxBlacklistedRounds from the current blacklist if possible.
// Otherwise it returns a new blacklist with GenesisMaxBlacklistedRounds max blacklisted rounds.
func (bl *Blacklist) Stateless(nodeCount uint16) Blacklist {
	if bl.maxBlacklistedRounds == 0 {
		return NewBlacklist(GenesisMaxBlacklistedRounds)
	}
	maxBlacklistedRounds := bl.maxBlacklistedRounds
	if maxBlacklistedRounds < nodeCount {
		maxBlacklistedRounds = nodeCount
	}
	return Blacklist{
		maxBlacklistedRounds: maxBlacklistedRounds,
	}
}

// Next returns the next blacklist for the given round, based on the current blacklist and the given timed out and redeemed nodes.
func (bl *Blacklist) Next(nodes Nodes, round uint64, maxBlacklistedRounds uint16, timedOut, redeemed map[uint16]uint64) Blacklist {
	weights := nodes.Weights()
	nodeCount := uint16(len(nodes))
	totalWeight := weights.Sum()
	updates := bl.computeBlacklistUpdates(round, nodeCount, timedOut, redeemed, totalWeight)
	newBlacklist := bl.applyUpdates(nodeCount, updates, round, weights, totalWeight)
	newBlacklist.maxBlacklistedRounds = bl.nextMaxBlacklistedRound(nodeCount, maxBlacklistedRounds)
	return newBlacklist
}

// nextMaxBlacklistedRound returns the maxBlacklistedRounds that a block built on top of this blacklist should have.
// It moves the current value by at most nodeCount rounds (a single orbit) towards the given target,
// and never below nodeCount, so a node is always blacklisted for at least one orbit.
// Once the current value is within nodeCount rounds of the target, it is set to the target (but not below nodeCount).
func (bl *Blacklist) nextMaxBlacklistedRound(nodeCount, target uint16) uint16 {
	current := bl.maxBlacklistedRounds

	// A value inherited from an epoch with fewer nodes may be below the current node count.
	if current < nodeCount {
		return nodeCount
	}

	// If the current value is within nodeCount rounds of the target, adopt the value.
	if diff(target, current) < nodeCount {
		return max(target, nodeCount)
	}

	// Otherwise, allow only nodeCount increments.

	if current < target {
		// target - current >= nodeCount, so this cannot overflow.
		return current + nodeCount
	}

	// current - target >= nodeCount, so this cannot underflow.
	return max(current-nodeCount, nodeCount)
}

func diff(a uint16, b uint16) uint16 {
	if a > b {
		return a - b
	}
	return b - a
}

// isValidMaxBlacklistedRoundsTransition returns whether the maxBlacklistedRounds can change from prev to next
// between a block and its child. The value can change by at most nodeCount rounds (a single orbit) and cannot go below nodeCount.
func isValidMaxBlacklistedRoundsTransition(prev, next uint16, nodeCount uint16) bool {
	if next < nodeCount {
		return false
	}
	return diff(prev, next) <= nodeCount
}

func (bl *Blacklist) String() string {
	sn := make([]string, len(bl.suspectedNodes))
	for i := range bl.suspectedNodes {
		sn[i] = bl.suspectedNodes[i].String()
	}

	return fmt.Sprintf(
		"Blacklist(maxBlacklistedRounds=%d, suspectedNodes=[%s], updates=[%s])",
		bl.maxBlacklistedRounds,
		strings.Join(sn, ","),
		blacklistUpdatesAsString(bl.updates), // helper receives array
	)
}

func blacklistUpdatesAsString(updates []BlacklistUpdate) string {
	out := make([]string, len(updates))
	for i := range updates {
		out[i] = updates[i].String()
	}
	return strings.Join(out, ",")
}

func (bl *Blacklist) Equals(b2 *Blacklist) bool {
	if bl.maxBlacklistedRounds != b2.maxBlacklistedRounds {
		return false
	}
	if len(bl.suspectedNodes) != len(b2.suspectedNodes) {
		return false
	}
	for i := range bl.suspectedNodes {
		sn1 := &bl.suspectedNodes[i]
		sn2 := &b2.suspectedNodes[i]
		if !sn1.Equals(sn2) {
			return false
		}
	}
	if len(bl.updates) != len(b2.updates) {
		return false
	}
	for i := range bl.updates {
		u1 := &bl.updates[i]
		u2 := &b2.updates[i]
		if !u1.Equals(u2) {
			return false
		}
	}
	return true
}

type BlacklistOpType uint8

const (
	BlacklistOpType_Undefined BlacklistOpType = iota
	BlacklistOpType_NodeSuspected
	BlacklistOpType_NodeRedeemed
)

type BlacklistUpdates []BlacklistUpdate

type BlacklistUpdate struct {
	Type      BlacklistOpType `canoto:"uint,1"`
	NodeIndex uint16          `canoto:"uint,2"`

	canotoData canotoData_BlacklistUpdate
}

func (bu *BlacklistUpdate) Equals(bu2 *BlacklistUpdate) bool {
	return bu.Type == bu2.Type && bu.NodeIndex == bu2.NodeIndex
}

func (bu *BlacklistUpdate) Clone() BlacklistUpdate {
	return BlacklistUpdate{
		Type:      bu.Type,
		NodeIndex: bu.NodeIndex,
	}
}

func (bu *BlacklistUpdate) String() string {
	switch bu.Type {
	case BlacklistOpType_NodeSuspected:
		return fmt.Sprintf("{type=NodeSuspected, NodeIndex=%d}", bu.NodeIndex)
	case BlacklistOpType_NodeRedeemed:
		return fmt.Sprintf("{type=NodeRedeemed, NodeIndex=%d}", bu.NodeIndex)
	default:
		return fmt.Sprintf("{type=Unknown(%d), NodeIndex=%d}", bu.Type, bu.NodeIndex)
	}
}

func (bl *Blacklist) IsEmpty() bool {
	return bl.maxBlacklistedRounds == 0
}

// ApplyUpdates applies the given updates in the given round to the current blacklist and returns a new blacklist.
// The current blacklist is not modified.
func (bl *Blacklist) applyUpdates(nodeCount uint16, updates []BlacklistUpdate, round uint64, weights uint64s, totalWeight uint64) Blacklist {
	newBlacklist := Blacklist{
		maxBlacklistedRounds: bl.maxBlacklistedRounds,
		suspectedNodes:       bl.garbageCollectSuspectedNodes(nodeCount, round, totalWeight),
		updates:              make([]BlacklistUpdate, len(updates)),
	}

	for i := range updates {
		update := &updates[i]
		orbit := orbit(round, update.NodeIndex, nodeCount)

		// Sanity check in case we get a too high node index.
		// This shouldn't happen, but if it does, we should just ignore it.
		if update.NodeIndex >= nodeCount || len(weights) != int(nodeCount) {
			continue
		}

		suspectingOrRedeemingNodeWeight := weights[round%uint64(nodeCount)]

		switch update.Type {
		case BlacklistOpType_NodeSuspected:
			newBlacklist.setNodeSuspected(orbit, update.NodeIndex, suspectingOrRedeemingNodeWeight, weights, totalWeight)
		case BlacklistOpType_NodeRedeemed:
			newBlacklist.setNodeRedeemed(orbit, update.NodeIndex, suspectingOrRedeemingNodeWeight, totalWeight)
		}
	}

	copy(newBlacklist.updates, updates)

	return newBlacklist
}

// garbageCollectSuspectedNodes returns a new list of suspected nodes for the given round.
// NodeIDs that are no longer suspected or have been redeemed, will not be included in the returned suspected nodes.
// It will also garbage-collect any redeem votes from past orbits, unless hey have surpassed the threshold of f+1.
// It does not modify the current blacklist.
func (bl *Blacklist) garbageCollectSuspectedNodes(nodeCount uint16, round uint64, totalWeight uint64) suspectedNodes {
	newSuspectedNodes := make([]SuspectedNode, 0, len(bl.suspectedNodes))
	threshold := bl.computeThreshold(totalWeight)

	for i := range bl.suspectedNodes {
		sn := &bl.suspectedNodes[i]
		orbit := orbit(round, sn.NodeIndex, nodeCount)
		// Node is no longer suspected, just skip it.
		if !sn.isStillSuspected(threshold, orbit) {
			continue
		}

		if orbit > sn.OrbitSuspected && orbit-sn.OrbitSuspected >= bl.maxBlacklistedOrbits(nodeCount) {
			// The node has been blacklisted for too many orbits, so we remove it from the suspected list.
			continue
		}

		newSuspectedNode := sn.Clone()

		//  Reset the redeemingCount and orbitToRedeem if it was a past orbit.
		if sn.OrbitToRedeem < orbit {
			newSuspectedNode.RedeemingWeight = 0
			newSuspectedNode.OrbitToRedeem = 0
		}
		newSuspectedNodes = append(newSuspectedNodes, newSuspectedNode)
	}

	return newSuspectedNodes
}

func (bl *Blacklist) setNodeSuspected(orbit uint64, nodeIndex uint16, suspectingOrRedeemingNodeWeight uint64, weights uint64s, totalWeight uint64) {
	// An orbit of 0 is a no-op
	if orbit == 0 {
		return
	}

	// First, look to see if the node is already suspected.
	// If it is, then we shouldn't further increment its count.
	if bl.IsNodeSuspected(nodeIndex, totalWeight) {
		return
	}

	threshold := bl.computeThreshold(totalWeight)

	var suspectedWeight uint64

	// Iterate through the suspected nodes to see if the node is already there.
	// If it is, increment its count and return.
	// If the node isn't there, we count the total number of suspected nodes.
	// If the total number of suspected nodes is >= f, we cannot suspect any more nodes.
	// Otherwise, we add the node to the suspected list with a count of 1.

	// Count how many total nodes are suspected.
	for i := range bl.suspectedNodes {
		sn := &bl.suspectedNodes[i]

		if sn.SuspectingWeight >= threshold {
			suspectedWeight += weights[sn.NodeIndex]
		}
	}

	if suspectedWeight+weights[nodeIndex] >= threshold {
		// We already have f or more nodes suspected, abort because we cannot suspect any more nodes.
		return
	}

	for i := range bl.suspectedNodes {
		sn := &bl.suspectedNodes[i]
		// Only increment the count if the orbit is the same.
		if sn.NodeIndex == nodeIndex {
			if sn.OrbitSuspected == orbit {
				// The node is already there, so increment its suspecting weight.
				sn.SuspectingWeight += suspectingOrRedeemingNodeWeight
				return
			}
			// Else, the orbit is different, so just return early.
			return
		}
	}

	// If we reached here, the node isn't in the suspected list, and it is small enough, so add it instead.
	bl.suspectedNodes = append(bl.suspectedNodes, SuspectedNode{
		NodeIndex:        nodeIndex,
		SuspectingWeight: suspectingOrRedeemingNodeWeight,
		OrbitSuspected:   orbit,
	})
}

func (bl *Blacklist) setNodeRedeemed(orbit uint64, nodeIndex uint16, suspectingOrRedeemingNodeWeight uint64, totalWeight uint64) {
	// An orbit of 0 is a no-op because a node cannot be suspected in orbit 0.
	if orbit == 0 {
		return
	}

	threshold := bl.computeThreshold(totalWeight)

	// First, look to see if the node is not suspected.
	// If it is not, then we shouldn't try to redeem it.
	if !bl.IsNodeSuspected(nodeIndex, totalWeight) {
		return
	}

	// Else, the node is suspected. Proceed to redeem it.
	for i := range bl.suspectedNodes {
		sn := &bl.suspectedNodes[i]
		// Only increment the count if the orbit is the same.
		if sn.NodeIndex == nodeIndex {
			if sn.OrbitSuspected == orbit {
				// It is too early to redeem a node that was suspected in the current orbit.
				return
			}
			if sn.OrbitToRedeem == 0 {
				// This is the first time the node is redeemed.
				sn.OrbitToRedeem = orbit
			}
			if sn.OrbitToRedeem == orbit {
				sn.RedeemingWeight += suspectingOrRedeemingNodeWeight
			}
			// If we have found a threshold of redeeming nodes, then we need to remove the node
			// from the suspected list.
			if sn.RedeemingWeight >= threshold {
				bl.suspectedNodes = slices.Delete(bl.suspectedNodes, i, i+1)
				return
			}
			// Else, the orbit is different, so just return early.
			return
		}
	}
}

func (bl *Blacklist) computeThreshold(totalWeight uint64) uint64 {
	threshold := totalWeight / 3
	if totalWeight%3 != 0 {
		threshold++
	}
	return threshold
}

func (bl *Blacklist) IsNodeSuspected(nodeIndex uint16, totalWeight uint64) bool {
	threshold := bl.computeThreshold(totalWeight)

	for i := range bl.suspectedNodes {
		sn := &bl.suspectedNodes[i]
		if sn.NodeIndex == nodeIndex {
			return sn.SuspectingWeight >= threshold && sn.RedeemingWeight < threshold
		}
	}

	return false
}

func (bl *Blacklist) Clone() Blacklist {
	clone := Blacklist{
		maxBlacklistedRounds: bl.maxBlacklistedRounds,
	}
	clone.suspectedNodes = make(suspectedNodes, 0, len(bl.suspectedNodes))
	for i := range bl.suspectedNodes {
		clone.suspectedNodes = append(clone.suspectedNodes, bl.suspectedNodes[i].Clone())
	}
	clone.updates = make(BlacklistUpdates, 0, len(bl.updates))
	for i := range bl.updates {
		clone.updates = append(clone.updates, bl.updates[i].Clone())
	}
	return clone
}

// Bytes returns the byte representation of the blacklist.
func (bl *Blacklist) Bytes() []byte {
	clone := bl.Clone()
	return clone.MarshalCanoto()
}

// FromBytes populates the blacklist from the given bytes.
func (bl *Blacklist) FromBytes(buff []byte) error {
	return bl.UnmarshalCanoto(buff)
}

func (bl *Blacklist) VerifyProposedBlacklist(nodes Nodes, candidateBlacklist Blacklist, round uint64) error {
	nodeCount := uint16(len(nodes))
	weights := nodes.Weights()

	if len(candidateBlacklist.suspectedNodes) > int(nodeCount) {
		return fmt.Errorf("%w: %d, only %d nodes exist", errBlacklistTooManySuspectedNodes, len(candidateBlacklist.suspectedNodes), nodeCount)
	}
	if !isValidMaxBlacklistedRoundsTransition(bl.maxBlacklistedRounds, candidateBlacklist.maxBlacklistedRounds, nodeCount) {
		return fmt.Errorf("%s, expected at least %d and within %d rounds of the previous value %d, got %d", errBlacklistInvalidMaxBlacklistedRounds, nodeCount, nodeCount, bl.maxBlacklistedRounds, candidateBlacklist.maxBlacklistedRounds)
	}
	// 1) First thing we check that the updates even make sense.
	totalWeight := weights.Sum()
	if err := bl.verifyBlacklistUpdates(nodeCount, candidateBlacklist.updates, totalWeight); err != nil {
		return fmt.Errorf("%s: %w", errBlacklistInvalidUpdates, err)
	}
	updates := candidateBlacklist.updates
	// 2) We then proceed by applying the updates to the blacklist of the previous round.
	// Garbage collection of expired suspected nodes uses the maxBlacklistedRounds of the previous round,
	// and the new maxBlacklistedRounds only takes effect in the next round.
	expectedBlacklist := bl.applyUpdates(nodeCount, updates, round, weights, totalWeight)
	expectedBlacklist.maxBlacklistedRounds = candidateBlacklist.maxBlacklistedRounds

	if !candidateBlacklist.Equals(&expectedBlacklist) {
		return fmt.Errorf("%s, expected %s, got %s", errBlacklistInvalidBlacklist, expectedBlacklist.String(), candidateBlacklist.String())
	}

	return nil
}

func (bl *Blacklist) verifyBlacklistUpdates(nodeCount uint16, updates []BlacklistUpdate, totalWeight uint64) error {
	seen := make(map[uint16]struct{})
	if len(updates) > int(nodeCount) {
		return fmt.Errorf("%w: %d, only %d nodes exist", errBlacklistTooManyUpdates, len(updates), nodeCount)
	}
	for i := range updates {
		update := &updates[i]
		if update.NodeIndex >= nodeCount {
			return fmt.Errorf("%w: %d, needs to be in [%d, %d]",
				errBlacklistInvalidNodeIndex, update.NodeIndex, 0, nodeCount-1)
		}

		if _, exists := seen[update.NodeIndex]; exists {
			return fmt.Errorf("%w: (%d)", errBlacklistNodeIndexAlreadyUpdated, update.NodeIndex)
		}
		seen[update.NodeIndex] = struct{}{}

		switch update.Type {
		case BlacklistOpType_NodeSuspected:
			// A node cannot be blacklisted if it is already suspected.
			if bl.IsNodeSuspected(update.NodeIndex, totalWeight) {
				return fmt.Errorf("%w: (%d)", errBlacklistNodeAlreadyBlacklisted, update.NodeIndex)
			}
		case BlacklistOpType_NodeRedeemed:
			// A node cannot be redeemed if it is not currently suspected.
			if !bl.IsNodeSuspected(update.NodeIndex, totalWeight) {
				return fmt.Errorf("%w: (%d)", errBlacklistNodeNotBlacklisted, update.NodeIndex)
			}
		default:
			return fmt.Errorf("%w: (%d)", errBlacklistInvalidUpdateType, update.Type)
		}
	}

	return nil
}

func (bl *Blacklist) computeBlacklistUpdates(round uint64, nodeCount uint16, timedOut, redeemed map[uint16]uint64, totalWeight uint64) []BlacklistUpdate {
	updates := make([]BlacklistUpdate, 0, nodeCount)

	for nodeIndex := uint16(0); nodeIndex < nodeCount; nodeIndex++ {
		timedOutInRound, isTimedOut := timedOut[nodeIndex]
		redeemedInRound, isRedeemed := redeemed[nodeIndex]

		if bl.IsNodeSuspected(nodeIndex, totalWeight) {
			// If the node wasn't redeemed, then we cannot redeem it.
			if !isRedeemed {
				continue
			}

			// If the round in which the node was lastly redeemed is not later than the round in which it timed out in,
			// then we cannot redeem the node.
			if redeemedInRound <= timedOutInRound {
				continue
			}

			if round < redeemedInRound {
				// This is a sanity check. This shouldn't happen, as round numbers only move forward.
				continue
			}

			roundsSinceLastRedeem := round - redeemedInRound

			if roundsSinceLastRedeem > uint64(nodeCount) {
				// The last time the node has redeemed was in an older orbit,
				// so ignore it. We can only redeem the node if it has redeemed in the current orbit.
				continue
			}

			updates = append(updates, BlacklistUpdate{
				NodeIndex: nodeIndex,
				Type:      BlacklistOpType_NodeRedeemed,
			})
		} else {
			// The node has yet to time out, so nothing suspicious here.
			if !isTimedOut {
				continue
			}

			// The node has redeemed itself after timing out, do not suspect it.
			if redeemedInRound > timedOutInRound {
				continue
			}

			if round < timedOutInRound {
				// This is a sanity check. This shouldn't happen, as round numbers only move forward.
				continue
			}

			roundsSinceLastTimeout := round - timedOutInRound

			if roundsSinceLastTimeout > uint64(nodeCount) {
				// The node has not timed out for a full orbit yet, so do not suspect it,
				// because it has lastly timed out in a past orbit.
				continue
			}

			updates = append(updates, BlacklistUpdate{
				NodeIndex: nodeIndex,
				Type:      BlacklistOpType_NodeSuspected,
			})
		}
	}
	return updates
}

type suspectedNodes []SuspectedNode

// SuspectedNode is the information we keep for each suspected node.
// A suspected node records the number of accusations and when it was accused in.
// A suspected node that has above f+1 accusations is considered as blacklisted.
// A suspected node that is blacklisted can be redeemed by gathering f+1 redeeming votes.
// A suspected node that has been redeemed by f+1 votes or more is removed from the blacklist.
type SuspectedNode struct {
	// NodeIndex is the index of the suspected node among the nodes of the validator set.
	NodeIndex uint16 `canoto:"uint,1"`
	// SuspectingWeight is the cumulative amount of weight of nodes that have suspected this node in the current orbit denoted by OrbitSuspected.
	// If this amount is > 33% of the total weight, then the node is considered blacklisted.
	SuspectingWeight uint64 `canoto:"uint,2"`
	// RedeemingCount is the number of nodes that have redeemed this node in the current orbit denoted by OrbitToRedeem.
	// If this amount is > 33% of the total weight, the node is removed from the blacklist.
	RedeemingWeight uint64 `canoto:"uint,3"`
	// OrbitSuspected is the orbit in which the node was last suspected.
	OrbitSuspected uint64 `canoto:"uint,4"`
	// OrbitToRedeem is the orbit in which the node was last redeemed.
	OrbitToRedeem uint64 `canoto:"uint,5"`

	canotoData canotoData_SuspectedNode
}

func (sn *SuspectedNode) Clone() SuspectedNode {
	return SuspectedNode{
		NodeIndex:        sn.NodeIndex,
		SuspectingWeight: sn.SuspectingWeight,
		RedeemingWeight:  sn.RedeemingWeight,
		OrbitSuspected:   sn.OrbitSuspected,
		OrbitToRedeem:    sn.OrbitToRedeem,
	}
}

func (sn *SuspectedNode) Equals(sn2 *SuspectedNode) bool {
	return sn.NodeIndex == sn2.NodeIndex &&
		sn.SuspectingWeight == sn2.SuspectingWeight &&
		sn.RedeemingWeight == sn2.RedeemingWeight &&
		sn.OrbitSuspected == sn2.OrbitSuspected &&
		sn.OrbitToRedeem == sn2.OrbitToRedeem
}

func (sn *SuspectedNode) String() string {
	return fmt.Sprintf("{NodeIndex=%d, SuspectingWeight=%d, RedeemingWeight=%d, OrbitSuspected=%d, OrbitToRedeem=%d}",
		sn.NodeIndex, sn.SuspectingWeight, sn.RedeemingWeight, sn.OrbitSuspected, sn.OrbitToRedeem)
}

// isStillSuspected returns true if the suspected node is still suspected in the given orbit.
func (sn *SuspectedNode) isStillSuspected(threshold uint64, orbit uint64) bool {
	if sn.OrbitSuspected >= orbit {
		// orbit hasn't advanced.
		return true
	}

	// Else, the orbit has advanced (sn.OrbitSuspected < orbit).
	if sn.SuspectingWeight < threshold {
		// The node is not blacklisted, so since we've advanced to a new orbit,
		// we can just drop it as not enough nodes suspected it.
		return false
	}

	// Else, the node is blacklisted. We need to see if it has been redeemed by enough nodes.
	if sn.RedeemingWeight >= threshold {
		return false
	}

	// Else, the node is still blacklisted.
	return true
}

func (bl *Blacklist) maxBlacklistedOrbits(nodeCount uint16) uint64 {
	if nodeCount == 0 {
		nodeCount = 1
	}
	return max(uint64(bl.maxBlacklistedRounds)/uint64(nodeCount), 1)
}
