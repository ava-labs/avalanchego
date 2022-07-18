// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

var _ StakerIterator = &sliceIterator{}

// Returns the elements of [stakers] in order. Doesn't sort by anything.
type sliceIterator struct {
	index   int
	stakers []*Staker
}

func NewSliceIterator(stakers ...*Staker) StakerIterator {
	return &sliceIterator{
		index:   -1,
		stakers: stakers,
	}
}

func (i *sliceIterator) Next() bool {
	i.index++
	return i.index < len(i.stakers)
}

func (i *sliceIterator) Value() *Staker {
	return i.stakers[i.index]
}

func (i *sliceIterator) Release() {}
