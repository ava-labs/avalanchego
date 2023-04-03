// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

var _ StakerIterator = (*maskedIterator)(nil)

type maskedIterator struct {
	// TODO ABENEGIA: an inefficient implementation for now
	sortedStakers []*Staker
	idx           int
}

// NewMaskedIterator returns a new iterator that skips the stakers in
// [parentIterator] that are present in [maskedStakers].

// Invariants
// maskedStakers and updatedStakers do not overlap
// updatedStakers are contained in parentIterator stakers

func NewMaskedIterator(parentIterator StakerIterator, maskedStakers, updatedStakers map[ids.ID]*Staker) StakerIterator {
	sortedStakers := make([]*Staker, 0)

	for parentIterator.Next() {
		staker := parentIterator.Value()
		if _, ok := maskedStakers[staker.TxID]; ok {
			continue // deleted element
		}
		if updated, ok := updatedStakers[staker.TxID]; ok {
			staker = updated
		}
		sortedStakers = append(sortedStakers, staker)
	}

	utils.Sort(sortedStakers)
	return &maskedIterator{
		sortedStakers: sortedStakers,
		idx:           -1,
	}
}

func (i *maskedIterator) Next() bool {
	i.idx++
	return i.idx < len(i.sortedStakers)
}

func (i *maskedIterator) Value() *Staker {
	return i.sortedStakers[i.idx]
}

func (*maskedIterator) Release() {}
