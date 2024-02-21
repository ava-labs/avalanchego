// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

type Interval struct {
	LowerBound uint64
	UpperBound uint64
}

func (i *Interval) Less(other *Interval) bool {
	return i.UpperBound < other.UpperBound
}

func (i *Interval) Contains(height uint64) bool {
	return i != nil && i.LowerBound <= height && height <= i.UpperBound
}

// AdjacentToUpperBound returns true if height is 1 greater than upperBound.
func (i *Interval) AdjacentToUpperBound(height uint64) bool {
	return i != nil && i.UpperBound+1 == height
}

// AdjacentToLowerBound returns true if height is 1 less than lowerBound.
func (i *Interval) AdjacentToLowerBound(height uint64) bool {
	return i != nil && height+1 == i.LowerBound
}
