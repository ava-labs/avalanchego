// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
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
		i.nextInLine[0] = nil
		i.nextInLine = i.nextInLine[1:]

		// insert nextParentStaker at the right position in i.nextInLine
		idx := 0
		for idx < len(i.nextInLine) && i.nextInLine[idx].Less(nextParentStaker) {
			idx++
		}
		if len(i.nextInLine) == idx {
			i.nextInLine = append(i.nextInLine, nextParentStaker)
		} else {
			i.nextInLine = append(i.nextInLine[:idx+1], i.nextInLine[idx:]...)
			i.nextInLine[idx] = nextParentStaker
		}
		return true
	}
}

func (i *maskedIterator) Value() *Staker {
	return i.nextStaker
}

func (i *maskedIterator) Release() {
	i.parentIterator.Release()
}
