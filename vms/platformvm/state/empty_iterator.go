// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

// EmptyIterator contains no stakers.
var EmptyIterator StakerIterator = emptyIterator{}

type emptyIterator struct{}

func (emptyIterator) Next() bool {
	return false
}

func (emptyIterator) Value() *Staker {
	return nil
}

func (emptyIterator) Release() {}
