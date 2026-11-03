// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"fmt"
	"math"
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
	errBlacklistInvalidNodeCount            = fmt.Errorf("block contains an invalid blacklist node count")
	errBlacklistInvalidWeightCount          = fmt.Errorf("block contains an invalid total weight node count")
	errBlacklistInvalidMaxBlacklistedOrbits = fmt.Errorf("block contains an invalid max blacklisted orbits")
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
	// Configuration: Below are configuration fields that
	// are present in every Blacklist.

	// nodeCount is the configuration of the blacklist.
	nodeCount uint16 `canoto:"uint,1"`

	// totalWeight is the total weight of the nodes in the validator set.
	totalWeight uint64 `canoto:"uint,2"`

	// maxBlacklistedOrbits is the maximum number of orbits a node can be blacklisted for.
	// Each block builder may move it by at most one orbit relative to the parent block,
	// towards the value in its local configuration, which allows it to be changed via a rolling upgrade.
	maxBlacklistedOrbits uint16 `canoto:"uint,3"`

	// State: Below are fields corresponding to state.
	// They are optional fields and may not be present
	// in a Blacklist.

	// suspectedNodes is the list of nodes that are currently suspected.
	// it's the inner state of the blacklist.
	suspectedNodes suspectedNodes `canoto:"repeated value,4"`

	// updates is the list of modifications that a block builder is proposing
	// to perform to the blacklist.
	updates BlacklistUpdates `canoto:"repeated value,5"`

	canotoData canotoData_Blacklist
}

func NewBlacklist(nodeCount uint16, totalWeight uint64, maxBlacklistedRounds uint16) Blacklist {
	return Blacklist{
		nodeCount:            nodeCount,
		totalWeight:          totalWeight,
		maxBlacklistedOrbits: maxBlacklistedOrbitsForRounds(nodeCount, maxBlacklistedRounds),
	}
}

// NodeCount returns the number of nodes the blacklist is configured for.
func (bl *Blacklist) NodeCount() uint16 {
	return bl.nodeCount
}

// TotalWeight returns the total weight of the nodes the blacklist is configured for.
func (bl *Blacklist) TotalWeight() uint64 {
	return bl.totalWeight
}

// MaxBlacklistedOrbits returns the maximum number of orbits a node can be blacklisted for.
func (bl *Blacklist) MaxBlacklistedOrbits() uint16 {
	return bl.maxBlacklistedOrbits
}

// maxBlacklistedOrbitsForRounds converts the given amount of rounds into whole orbits of the given node count.
// The result is at least one orbit.
func maxBlacklistedOrbitsForRounds(nodeCount uint16, maxBlacklistedRounds uint16) uint16 {
	denominator := nodeCount

	// Protection against division by zero
	if denominator == 0 {
		denominator = 1
	}

	// If the node count is bigger than maxBlacklistedRounds,
	// we return 1.
	return max(maxBlacklistedRounds/denominator, 1)
}

// Stateless returns a new blacklist with the same configuration as the current blacklist, but without any state.
// It derives the maxBlacklistedOrbits from the current blacklist, rescaled to the new node count, if possible.
// Otherwise it returns a new blacklist with the given node count and GenesisMaxBlacklistedRounds.
func (bl *Blacklist) Stateless(nodeCount uint16, totalWeight uint64) Blacklist {
	if bl.maxBlacklistedOrbits == 0 {
		return NewBlacklist(nodeCount, totalWeight, GenesisMaxBlacklistedRounds)
	}
	return Blacklist{
		nodeCount:            nodeCount,
		totalWeight:          totalWeight,
		maxBlacklistedOrbits: rescaleMaxBlacklistedOrbits(bl.maxBlacklistedOrbits, bl.nodeCount, nodeCount),
	}
}

// Next returns the next blacklist for the given round, based on the current blacklist and the given timed out and redeemed nodes.
func (bl *Blacklist) Next(round uint64, maxBlacklistedRounds uint16, weights []uint64, timedOut, redeemed map[uint16]uint64) Blacklist {
	updates := bl.computeBlacklistUpdates(round, bl.nodeCount, timedOut, redeemed)
	newBlacklist := bl.applyUpdates(updates, round, weights)
	targetOrbits := maxBlacklistedOrbitsForRounds(bl.nodeCount, maxBlacklistedRounds)
	newBlacklist.maxBlacklistedOrbits = bl.nextMaxBlacklistedOrbits(targetOrbits)
	return newBlacklist
}

// rescaleMaxBlacklistedOrbits converts the given amount of orbits of prevNodeCount into whole orbits of nodeCount,
// such that it corresponds to (roughly) the same number of rounds.
// The result is at least one orbit, and at most math.MaxUint16.
func rescaleMaxBlacklistedOrbits(orbits uint16, prevNodeCount uint16, nodeCount uint16) uint16 {
	// Protection against division by zero, and if we don't know the previous node count, we keep the orbits as is.
	if nodeCount == 0 || prevNodeCount == 0 {
		return max(orbits, 1)
	}

	rounds := uint64(orbits) * uint64(prevNodeCount)
	rescaled := min(rounds/uint64(nodeCount), math.MaxUint16)
	return max(uint16(rescaled), 1)
}

// nextMaxBlacklistedOrbits returns the maxBlacklistedOrbits that a block built on top of this blacklist should have.
// It moves the current value by a single orbit towards the given target, or keeps it if it is already equal to the target.
func (bl *Blacklist) nextMaxBlacklistedOrbits(target uint16) uint16 {
	current := bl.maxBlacklistedOrbits
	switch {
	case current < target:
		return current + 1
	case current > target && current > 1:
		return current - 1
	default:
		return current
	}
}

// isValidMaxBlacklistedOrbitsTransition returns whether the maxBlacklistedOrbits can change from prev to next
// between a block and its child. The value can change by at most one orbit, and it must be at least one orbit.
func isValidMaxBlacklistedOrbitsTransition(prev, next uint16) bool {
	if next == 0 {
		return false
	}
	return next == prev || uint32(next) == uint32(prev)+1 || uint32(next)+1 == uint32(prev)
}

func (bl *Blacklist) String() string {
	sn := make([]string, len(bl.suspectedNodes))
	for i := range bl.suspectedNodes {
		sn[i] = bl.suspectedNodes[i].String()
	}

	return fmt.Sprintf(
		"Blacklist(nodeCount=%d, totalWeight=%d, maxBlacklistedOrbits=%d, suspectedNodes=[%s], updates=[%s])",
		bl.nodeCount,
		bl.totalWeight,
		bl.maxBlacklistedOrbits,
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
	if bl.nodeCount != b2.nodeCount {
		return false
	}
	if bl.totalWeight != b2.totalWeight {
		return false
	}
	if bl.maxBlacklistedOrbits != b2.maxBlacklistedOrbits {
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
	return bl.nodeCount == 0
}

// ApplyUpdates applies the given updates in the given round to the current blacklist and returns a new blacklist.
// The current blacklist is not modified.
func (bl *Blacklist) applyUpdates(updates []BlacklistUpdate, round uint64, weights []uint64) Blacklist {
	newBlacklist := Blacklist{
		totalWeight:          bl.totalWeight,
		nodeCount:            bl.nodeCount,
		maxBlacklistedOrbits: bl.maxBlacklistedOrbits,
		suspectedNodes:       make(suspectedNodes, len(bl.suspectedNodes)),
		updates:              make([]BlacklistUpdate, len(updates)),
	}

	newBlacklist.suspectedNodes = bl.garbageCollectSuspectedNodes(round)

	for i := range updates {
		update := &updates[i]
		orbit := orbit(round, update.NodeIndex, bl.nodeCount)

		// Sanity check in case we get a too high node index.
		// This shouldn't happen, but if it does, we should just ignore it.
		if update.NodeIndex >= bl.nodeCount || len(weights) != int(bl.nodeCount) {
			continue
		}

		suspectingOrRedeemingNodeWeight := weights[round%uint64(bl.nodeCount)]

		switch update.Type {
		case BlacklistOpType_NodeSuspected:
			newBlacklist.setNodeSuspected(orbit, update.NodeIndex, suspectingOrRedeemingNodeWeight, weights)
		case BlacklistOpType_NodeRedeemed:
			newBlacklist.setNodeRedeemed(orbit, update.NodeIndex, suspectingOrRedeemingNodeWeight)
		}
	}

	copy(newBlacklist.updates, updates)

	return newBlacklist
}

// garbageCollectSuspectedNodes returns a new list of suspected nodes for the given round.
// NodeIDs that are no longer suspected or have been redeemed, will not be included in the returned suspected nodes.
// It will also garbage-collect any redeem votes from past orbits, unless hey have surpassed the threshold of f+1.
// It does not modify the current blacklist.
func (bl *Blacklist) garbageCollectSuspectedNodes(round uint64) suspectedNodes {
	newSuspectedNodes := make([]SuspectedNode, 0, len(bl.suspectedNodes))
	threshold := bl.computeThreshold()

	for i := range bl.suspectedNodes {
		sn := &bl.suspectedNodes[i]
		orbit := orbit(round, sn.NodeIndex, bl.nodeCount)
		// Node is no longer suspected, just skip it.
		if !sn.isStillSuspected(threshold, orbit) {
			continue
		}

		if orbit > sn.OrbitSuspected && orbit-sn.OrbitSuspected >= uint64(bl.maxBlacklistedOrbits) {
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

func (bl *Blacklist) setNodeSuspected(orbit uint64, nodeIndex uint16, suspectingOrRedeemingNodeWeight uint64, weights []uint64) {
	// An orbit of 0 is a no-op
	if orbit == 0 {
		return
	}

	// First, look to see if the node is already suspected.
	// If it is, then we shouldn't further increment its count.
	if bl.IsNodeSuspected(nodeIndex) {
		return
	}

	threshold := bl.computeThreshold()

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

func (bl *Blacklist) setNodeRedeemed(orbit uint64, nodeIndex uint16, suspectingOrRedeemingNodeWeight uint64) {
	// An orbit of 0 is a no-op because a node cannot be suspected in orbit 0.
	if orbit == 0 {
		return
	}

	threshold := bl.computeThreshold()

	// First, look to see if the node is not suspected.
	// If it is not, then we shouldn't try to redeem it.
	if !bl.IsNodeSuspected(nodeIndex) {
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

func (bl *Blacklist) computeThreshold() uint64 {
	threshold := bl.totalWeight / 3
	if bl.totalWeight%3 != 0 {
		threshold++
	}
	return threshold
}

func (bl *Blacklist) IsNodeSuspected(nodeIndex uint16) bool {
	threshold := bl.computeThreshold()

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
		totalWeight:          bl.totalWeight,
		nodeCount:            bl.nodeCount,
		maxBlacklistedOrbits: bl.maxBlacklistedOrbits,
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

func (bl *Blacklist) VerifyProposedBlacklist(candidateBlacklist Blacklist, round uint64, weights uint64s) error {
	if candidateBlacklist.nodeCount != bl.nodeCount {
		return fmt.Errorf("%s, expected %d, got %d", errBlacklistInvalidNodeCount, bl.nodeCount, candidateBlacklist.nodeCount)
	}
	if candidateBlacklist.totalWeight != bl.totalWeight {
		return fmt.Errorf("%s, expected %d, got %d", errBlacklistInvalidWeightCount, bl.totalWeight, candidateBlacklist.totalWeight)
	}
	if len(candidateBlacklist.suspectedNodes) > int(bl.nodeCount) {
		return fmt.Errorf("%w: %d, only %d nodes exist", errBlacklistTooManySuspectedNodes, len(candidateBlacklist.suspectedNodes), bl.nodeCount)
	}
	if !isValidMaxBlacklistedOrbitsTransition(bl.maxBlacklistedOrbits, candidateBlacklist.maxBlacklistedOrbits) {
		return fmt.Errorf("%s, expected a non-zero value within one orbit of %d, got %d", errBlacklistInvalidMaxBlacklistedOrbits, bl.maxBlacklistedOrbits, candidateBlacklist.maxBlacklistedOrbits)
	}
	// 1) First thing we check that the updates even make sense.
	if err := bl.verifyBlacklistUpdates(candidateBlacklist.updates); err != nil {
		return fmt.Errorf("%s: %w", errBlacklistInvalidUpdates, err)
	}
	updates := candidateBlacklist.updates
	// 2) We then proceed by applying the updates to the blacklist of the previous round.
	// Garbage collection of expired suspected nodes uses the maxBlacklistedOrbits of the previous round,
	// and the new maxBlacklistedOrbits only takes effect in the next round.
	expectedBlacklist := bl.applyUpdates(updates, round, weights)
	expectedBlacklist.maxBlacklistedOrbits = candidateBlacklist.maxBlacklistedOrbits

	if !candidateBlacklist.Equals(&expectedBlacklist) {
		return fmt.Errorf("%s, expected %s, got %s", errBlacklistInvalidBlacklist, expectedBlacklist.String(), candidateBlacklist.String())
	}

	return nil
}

func (bl *Blacklist) verifyBlacklistUpdates(updates []BlacklistUpdate) error {
	seen := make(map[uint16]struct{})
	if len(updates) > int(bl.nodeCount) {
		return fmt.Errorf("%w: %d, only %d nodes exist", errBlacklistTooManyUpdates, len(updates), bl.nodeCount)
	}
	for i := range updates {
		update := &updates[i]
		if update.NodeIndex >= bl.nodeCount {
			return fmt.Errorf("%w: %d, needs to be in [%d, %d]",
				errBlacklistInvalidNodeIndex, update.NodeIndex, 0, bl.nodeCount-1)
		}

		if _, exists := seen[update.NodeIndex]; exists {
			return fmt.Errorf("%w: (%d)", errBlacklistNodeIndexAlreadyUpdated, update.NodeIndex)
		}
		seen[update.NodeIndex] = struct{}{}

		switch update.Type {
		case BlacklistOpType_NodeSuspected:
			// A node cannot be blacklisted if it is already suspected.
			if bl.IsNodeSuspected(update.NodeIndex) {
				return fmt.Errorf("%w: (%d)", errBlacklistNodeAlreadyBlacklisted, update.NodeIndex)
			}
		case BlacklistOpType_NodeRedeemed:
			// A node cannot be redeemed if it is not currently suspected.
			if !bl.IsNodeSuspected(update.NodeIndex) {
				return fmt.Errorf("%w: (%d)", errBlacklistNodeNotBlacklisted, update.NodeIndex)
			}
		default:
			return fmt.Errorf("%w: (%d)", errBlacklistInvalidUpdateType, update.Type)
		}
	}

	return nil
}

func (bl *Blacklist) computeBlacklistUpdates(round uint64, nodeCount uint16, timedOut, redeemed map[uint16]uint64) []BlacklistUpdate {
	updates := make([]BlacklistUpdate, 0, nodeCount)

	for nodeIndex := uint16(0); nodeIndex < nodeCount; nodeIndex++ {
		timedOutInRound, isTimedOut := timedOut[nodeIndex]
		redeemedInRound, isRedeemed := redeemed[nodeIndex]

		if bl.IsNodeSuspected(nodeIndex) {
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
