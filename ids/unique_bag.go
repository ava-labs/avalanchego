// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"strings"
)

const (
	minUniqueBagSize = 16
)

// UniqueBag ...
type UniqueBag map[ID]BitSet

func (b *UniqueBag) init() {
	if *b == nil {
		*b = make(map[ID]BitSet, minUniqueBagSize)
	}
}

// Add ...
func (b *UniqueBag) Add(setID uint, idSet ...ID) {
	bs := BitSet(0)
	bs.Add(setID)

	for _, id := range idSet {
		b.UnionSet(id, bs)
	}
}

// UnionSet ...
func (b *UniqueBag) UnionSet(id ID, set BitSet) {
	b.init()

	previousSet := (*b)[id]
	previousSet.Union(set)
	(*b)[id] = previousSet
}

// DifferenceSet ...
func (b *UniqueBag) DifferenceSet(id ID, set BitSet) {
	b.init()

	previousSet := (*b)[id]
	previousSet.Difference(set)
	(*b)[id] = previousSet
}

// Difference ...
func (b *UniqueBag) Difference(diff *UniqueBag) {
	b.init()

	for id, previousSet := range *b {
		if previousSetDiff, exists := (*diff)[id]; exists {
			previousSet.Difference(previousSetDiff)
		}
		(*b)[id] = previousSet
	}
}

// GetSet ...
func (b *UniqueBag) GetSet(id ID) BitSet { return (*b)[id] }

// RemoveSet ...
func (b *UniqueBag) RemoveSet(id ID) { delete(*b, id) }

// List ...
func (b *UniqueBag) List() []ID {
	idList := make([]ID, len(*b))
	i := 0
	for id := range *b {
		idList[i] = id
		i++
	}
	return idList
}

// Bag ...
func (b *UniqueBag) Bag(alpha int) Bag {
	bag := Bag{
		counts: make(map[ID]int, len(*b)),
	}
	bag.SetThreshold(alpha)
	for id, bs := range *b {
		bag.AddCount(id, bs.Len())
	}
	return bag
}

func (b *UniqueBag) String() string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("UniqueBag: (Size = %d)", len(*b)))
	for id, set := range *b {
		sb.WriteString(fmt.Sprintf("\n    ID[%s]: Members = %s", id, set))
	}

	return sb.String()
}
