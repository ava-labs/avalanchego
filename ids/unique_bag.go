// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"strings"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	minUniqueBagSize = 16
)

type UniqueBag map[ID]set.Bits64

func (b *UniqueBag) init() {
	if *b == nil {
		*b = make(map[ID]set.Bits64, minUniqueBagSize)
	}
}

func (b *UniqueBag) Add(setID uint, idSet ...ID) {
	bs := set.Bits64(0)
	bs.Add(setID)

	for _, id := range idSet {
		b.UnionSet(id, bs)
	}
}

func (b *UniqueBag) UnionSet(id ID, set set.Bits64) {
	b.init()

	previousSet := (*b)[id]
	previousSet.Union(set)
	(*b)[id] = previousSet
}

func (b *UniqueBag) DifferenceSet(id ID, set set.Bits64) {
	b.init()

	previousSet := (*b)[id]
	previousSet.Difference(set)
	(*b)[id] = previousSet
}

func (b *UniqueBag) Difference(diff *UniqueBag) {
	b.init()

	for id, previousSet := range *b {
		if previousSetDiff, exists := (*diff)[id]; exists {
			previousSet.Difference(previousSetDiff)
		}
		(*b)[id] = previousSet
	}
}

func (b *UniqueBag) GetSet(id ID) set.Bits64 {
	return (*b)[id]
}

func (b *UniqueBag) RemoveSet(id ID) {
	delete(*b, id)
}

func (b *UniqueBag) List() []ID {
	return maps.Keys(*b)
}

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

func (b *UniqueBag) PrefixedString(prefix string) string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("UniqueBag: (Size = %d)", len(*b)))
	for id, set := range *b {
		sb.WriteString(fmt.Sprintf("\n%s    ID[%s]: Members = %s", prefix, id, set))
	}

	return sb.String()
}

func (b *UniqueBag) String() string {
	return b.PrefixedString("")
}

func (b *UniqueBag) Clear() {
	maps.Clear(*b)
}
