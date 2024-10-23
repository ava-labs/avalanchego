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
	// prefixGroups that correspond to zero and one being the first bit of their members, respectively.
	zg, og *prefixGroup
	// the prefixGroup that this prefixGroup was split from.
	parent *prefixGroup
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

			// Backup the parent
			parent := pg.parent
			// Become your descendant, but keep your original parent
			*pg = *descendant
			pg.parent = parent
		}

		if !couldSplit {
			break
		}
	}

	return originPG
}

func determineDescendant(pg *prefixGroup) *prefixGroup {
	var descendant *prefixGroup

	if pg.zg == nil && pg.og == nil {
		// If both are nil, it's a programming error, so panic.
		panic("programming error: both zero group and one group are nil")
	}

	if pg.zg != nil {
		descendant = pg.zg
	}

	if pg.og != nil {
		descendant = pg.og
	}
	return descendant
}

// bifurcationsWithCommonPrefix invokes f() on this and descendant prefixGroups
// which represent common prefixes and not an ID in its entirety.
func (pg *prefixGroup) bifurcationsWithCommonPrefix(f func([]ids.ID, []uint8)) {
	pg.traverse(func(prefixGroup *prefixGroup) {
		if prefixGroup.isBifurcation() && len(prefixGroup.prefix) > 0 {
			f(prefixGroup.members, prefixGroup.prefix)
		}
	})
}

// isBifurcation returns whether this prefixGroup has both zero and one bit descendants.
func (pg *prefixGroup) isBifurcation() bool {
	return pg.zg != nil && pg.og != nil
}

// canSplit returns whether this prefixGroup can be split.
func (pg *prefixGroup) canSplit() bool {
	return !pg.wasSplit && len(pg.members) > 1
}

// traverse invokes f() on this prefixGroup and all descendants in pre-order traversal.
func (pg *prefixGroup) traverse(f func(*prefixGroup)) {
	f(pg)
	if pg.zg != nil {
		pg.zg.traverse(f)
	}
	if pg.og != nil {
		pg.og.traverse(f)
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
		parent:  pg,
		index:   pg.index + 1,
		prefix:  make([]uint8, len(pg.prefix)+1),
		members: make([]ids.ID, 0, len(pg.members)),
	}
	og := &prefixGroup{
		parent:  pg,
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

	pg.og = og
	pg.zg = zg
	pg.wasSplit = true

	return zg, og
}
