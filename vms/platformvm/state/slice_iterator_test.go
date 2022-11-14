// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

var _ StakerIterator = (*sliceIterator)(nil)

type sliceIterator struct {
	index   int
	stakers []*Staker
}

// NewSliceIterator returns an iterator that contains the elements of [stakers]
// in order. Doesn't sort by anything.
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

func (*sliceIterator) Release() {}
