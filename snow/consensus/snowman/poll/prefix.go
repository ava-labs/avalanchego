// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"
	"github.com/ava-labs/avalanchego/ids"
)

// prefixGroup represents a bunch of IDs (stored in the members field),
// with a bit prefix.
// Each time the prefixGroup is split, it is divided into one or more prefixGroups
// according to the next bit index in the index field.
// Successively splitting prefixGroups yields a graph, with the first prefixGroup as the root.
type prefixGroup struct {
	// the bit index this prefixGroup would be split on by the next invocation of split().
	index int
	// the bits of the members of this prefixGroup from the first bit to the bit index.
	prefix []uint8
	// the IDs of the prefixGroup
	members []ids.ID
	// children are prefixGroups that correspond to zero and one being the first bit of their members, respectively.
	children [2]*prefixGroup
	// was this prefixGroup split before. Used to prevent a prefixGroup from being split more than once,
	// otherwise longestSharedPrefixes() would run indefinitely.
	wasSplit bool
}

// longestSharedPrefixes creates a prefixGroup that is the root of a graph
// of prefixGroup vertices.
// When iterating the graph, each prefixGroup vertex represents a shared bit prefix
// of IDs, and the members field contains all IDs from the given idList for which their bit prefix
// matches the prefix field.
func longestSharedPrefixes(idList []ids.ID) *prefixGroup {
	// First thing - de-duplicate all ids that appear twice or more
	idList = deduplicate(idList)

	originPG := &prefixGroup{members: idList}

	pgs := make([]*prefixGroup, 0, len(idList))
	pgs = append(pgs, originPG)

	// Try to split each prefix group.
	// Continue until all prefix groups cannot be split anymore.
	for {
		var couldSplit bool
		for _, pg := range pgs {
			if !pg.canSplit() {
				continue
			}

			couldSplit = true

			pg0, pg1 := pg.split()

			if pg0 != nil && pg1 != nil {
				pgs = append(pgs, pg0)
				pgs = append(pgs, pg1)
				continue
			}

			// Else, there is no bifurcation,
			// so swallow up your descendant
			descendant := determineDescendant(pg)

			// Become your descendant
			*pg = *descendant
		}

		if !couldSplit {
			break
		}
	}

	return originPG
}

func determineDescendant(pg *prefixGroup) *prefixGroup {
	var descendant *prefixGroup

	zg := pg.children[0]
	og := pg.children[1]

	if zg == nil && og == nil {
		// If both are nil, it's a programming error, so panic.
		panic("programming error: both zero group and one group are nil")
	}

	if zg != nil {
		descendant = zg
	}

	if og != nil {
		descendant = og
	}
	return descendant
}

// bifurcationsWithCommonPrefix traverses the transitive descendants of this prefix group,
// and applies f() on the block IDs of each prefix group.
// Prefix groups with no descendants are skipped, as they do not represent any prefix.
// Prefix group without a prefix (root prefix group) are also skipped as they do not correspond
// to any instance of snowflake.
func (pg *prefixGroup) bifurcationsWithCommonPrefix(f func([]ids.ID)) {
	pg.traverse(func(prefixGroup *prefixGroup) {
		if prefixGroup.isBifurcation() && len(prefixGroup.prefix) > 0 {
			f(prefixGroup.members)
		}
	})
}

// isBifurcation returns whether this prefixGroup has both zero and one bit descendants.
func (pg *prefixGroup) isBifurcation() bool {
	return pg.children[0] != nil && pg.children[1] != nil
}

// canSplit returns whether this prefixGroup can be split.
func (pg *prefixGroup) canSplit() bool {
	return !pg.wasSplit && len(pg.members) > 1
}

// traverse invokes f() on this prefixGroup and all descendants in pre-order traversal.
func (pg *prefixGroup) traverse(f func(*prefixGroup)) {
	f(pg)
	for _, childPG := range pg.children {
		if childPG != nil {
			childPG.traverse(f)
		}
	}
}

// split splits the prefixGroup into two prefixGroups according
// to members and the next internal bit.
// All members in the current prefixGroup with bit zero in the next bit index are returned
// in the left result, and similarly for the bit one for the right result.
// Invariant: As long as the current prefixGroup can be split (canSplit() returns true),
// If canSplit() returned true on this prefixGroup, split() will never return (nil, nil),
// since it has at least two members, which means they either differ in the next bit index,
// in which case two prefixGroups would be returned, and otherwise they do not differ
// in the next bit, and then at least one prefixGroup would be returned.
func (pg *prefixGroup) split() (*prefixGroup, *prefixGroup) {
	zg := &prefixGroup{
		index:   pg.index + 1,
		prefix:  make([]uint8, len(pg.prefix)+1),
		members: make([]ids.ID, 0, len(pg.members)),
	}
	og := &prefixGroup{
		index:   pg.index + 1,
		prefix:  make([]uint8, len(pg.prefix)+1),
		members: make([]ids.ID, 0, len(pg.members)),
	}

	copy(zg.prefix, pg.prefix)
	copy(og.prefix, pg.prefix)
	zg.prefix[len(zg.prefix)-1] = 0
	og.prefix[len(og.prefix)-1] = 1

	// Split members according to their next bit
	for _, member := range pg.members {
		bit := member.Bit(uint(pg.index))
		switch bit {
		case 0:
			zg.members = append(zg.members, member)
		case 1:
			og.members = append(og.members, member)
		default:
			// This is a sanity check which ensures that both zg.members and og.members cannot be empty.
			panic(fmt.Sprintf("programming error: the %d bit of %s is %d", pg.index, member, bit))
		}
	}

	if len(og.members) == 0 {
		og = nil
	}

	if len(zg.members) == 0 {
		zg = nil
	}

	pg.children[0] = zg
	pg.children[1] = og

	pg.wasSplit = true

	return zg, og
}

func deduplicate(in []ids.ID) []ids.ID {
	out := make([]ids.ID, 0, len(in))
	used := make(map[ids.ID]struct{})
	for _, id := range in {
		if _, exists := used[id]; exists {
			continue
		}
		used[id] = struct{}{}
		out = append(out, id)
	}

	return out
}
