// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"strings"
)

// ShortBag is a multiset of ShortIDs.
type ShortBag struct {
	counts map[ShortID]int
	size   int
}

func (b *ShortBag) init() {
	if b.counts == nil {
		b.counts = make(map[ShortID]int, minBagSize)
	}
}

// Add increases the number of times each id has been seen by one.
func (b *ShortBag) Add(ids ...ShortID) {
	for _, id := range ids {
		b.AddCount(id, 1)
	}
}

// AddCount increases the nubmer of times the id has been seen by count.
//
// count must be >= 0
func (b *ShortBag) AddCount(id ShortID, count int) {
	if count <= 0 {
		return
	}

	b.init()

	totalCount := b.counts[id] + count
	b.counts[id] = totalCount
	b.size += count
}

// Count returns the number of times the id has been added.
func (b *ShortBag) Count(id ShortID) int {
	b.init()
	return b.counts[id]
}

// Remove sets the count of the provided ID to zero.
func (b *ShortBag) Remove(id ShortID) {
	b.init()
	count := b.counts[id]
	delete(b.counts, id)
	b.size -= count
}

// Len returns the number of times an id has been added.
func (b *ShortBag) Len() int { return b.size }

// List returns a list of all ids that have been added.
func (b *ShortBag) List() []ShortID {
	idList := make([]ShortID, len(b.counts))
	i := 0
	for id := range b.counts {
		idList[i] = id
		i++
	}
	return idList
}

// Equals returns true if the bags contain the same elements
func (b *ShortBag) Equals(oIDs ShortBag) bool {
	if b.Len() != oIDs.Len() {
		return false
	}
	for key, value := range b.counts {
		if value != oIDs.counts[key] {
			return false
		}
	}
	return true
}

func (b *ShortBag) PrefixedString(prefix string) string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Bag: (Size = %d)", b.Len()))
	for id, count := range b.counts {
		sb.WriteString(fmt.Sprintf("\n%s    ID[%s]: Count = %d", prefix, id, count))
	}

	return sb.String()
}

func (b *ShortBag) String() string { return b.PrefixedString("") }
