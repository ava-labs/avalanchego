// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ StakerIterator = (*maskedIterator)(nil)

type maskedIterator struct {
	parentIterator StakerIterator
	maskedStakers  map[ids.ID]*Staker
}

// NewMaskedIterator returns a new iterator that skips the stakers in
// [parentIterator] that are present in [maskedStakers].
func NewMaskedIterator(parentIterator StakerIterator, maskedStakers map[ids.ID]*Staker) StakerIterator {
	return &maskedIterator{
		parentIterator: parentIterator,
		maskedStakers:  maskedStakers,
	}
}

func (i *maskedIterator) Next() bool {
	for i.parentIterator.Next() {
		staker := i.parentIterator.Value()
		if _, ok := i.maskedStakers[staker.TxID]; !ok {
			return true
		}
	}
	return false
}

func (i *maskedIterator) Value() *Staker {
	return i.parentIterator.Value()
}

func (i *maskedIterator) Release() {
	i.parentIterator.Release()
}
