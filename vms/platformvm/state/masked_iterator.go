// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"golang.org/x/exp/maps"
)

var _ StakerIterator = (*maskedIterator)(nil)

type maskedIterator struct {
	parentIterator StakerIterator
	maskedStakers  map[ids.ID]*Staker
	updatedStakers map[ids.ID]*Staker

	nextStaker *Staker
	nextInLine []*Staker
}

// NewMaskedIterator returns a new iterator that skips the stakers in
// [parentIterator] that are present in [maskedStakers].

// Invariants
// maskedStakers and updatedStakers do not overlap
// all updatedStakers are contained in parentIterator stakers
// if parentStaker has an updated version, say updatedStaker, then parentStaker.Less(updatedStaker)

func NewMaskedIterator(parentIterator StakerIterator, maskedStakers, updatedStakers map[ids.ID]*Staker) StakerIterator {
	nextInLine := maps.Values(updatedStakers)
	utils.Sort(nextInLine)

	return &maskedIterator{
		parentIterator: parentIterator,
		maskedStakers:  maskedStakers,
		updatedStakers: updatedStakers,
		nextStaker:     nil,
		nextInLine:     nextInLine,
	}
}

func (i *maskedIterator) Next() bool {
	var nextParentStaker *Staker
	for i.parentIterator.Next() {
		staker := i.parentIterator.Value()
		if _, ok := i.maskedStakers[staker.TxID]; ok {
			continue
		}
		if _, ok := i.updatedStakers[staker.TxID]; ok {
			continue
		}
		nextParentStaker = staker
		break
	}

	switch {
	case nextParentStaker == nil && len(i.nextInLine) == 0:
		return false // done iteration
	case nextParentStaker == nil && len(i.nextInLine) != 0:
		i.nextStaker = i.nextInLine[0]
		i.nextInLine[0] = nil
		i.nextInLine = i.nextInLine[1:]
		return true
	case nextParentStaker != nil && len(i.nextInLine) == 0:
		i.nextStaker = nextParentStaker
		return true
	default:
		// nextParentStaker != nil && len(i.nextInLine) != 0
		nextInLine := i.nextInLine[0]
		if nextParentStaker.Less(nextInLine) {
			i.nextStaker = nextParentStaker
			return true
		}

		i.nextStaker = nextInLine
		i.nextInLine = i.nextInLine[1:]
		i.nextInLine = append(i.nextInLine, nextParentStaker)
		utils.Sort(i.nextInLine)
		return true
	}
}

func (i *maskedIterator) Value() *Staker {
	return i.nextStaker
}

func (i *maskedIterator) Release() {
	i.parentIterator.Release()
}
