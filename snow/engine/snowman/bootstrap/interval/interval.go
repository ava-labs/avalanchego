// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

type interval struct {
	lowerBound uint64
	upperBound uint64
}

func (i *interval) Less(other *interval) bool {
	return i.upperBound < other.upperBound
}

func (i *interval) Contains(height uint64) bool {
	return i != nil && i.lowerBound <= height && height <= i.upperBound
}

// AdjacentToUpperBound returns true if height is 1 greater than upperBound.
func (i *interval) AdjacentToUpperBound(height uint64) bool {
	return i != nil && i.upperBound+1 == height
}

// AdjacentToLowerBound returns true if height is 1 less than lowerBound.
func (i *interval) AdjacentToLowerBound(height uint64) bool {
	return i != nil && height+1 == i.lowerBound
}
