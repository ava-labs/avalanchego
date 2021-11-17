// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUniqueBag(t *testing.T) {
	var ub1 UniqueBag

	ub1.init()

	if ub1 == nil {
		t.Fatalf("Unique Bag still nil after initialized")
	} else if len(ub1.List()) != 0 {
		t.Fatalf("Unique Bag should be empty")
	}

	id1 := Empty.Prefix(1)
	id2 := Empty.Prefix(2)

	ub2 := make(UniqueBag)
	ub2.Add(1, id1, id2)

	if !ub2.GetSet(id1).Contains(1) {
		t.Fatalf("Set missing element")
	} else if !ub2.GetSet(id2).Contains(1) {
		t.Fatalf("Set missing element")
	}

	var bs1 BitSet
	bs1.Add(2)
	bs1.Add(4)

	ub3 := make(UniqueBag)

	ub3.UnionSet(id1, bs1)

	bs1.Clear()
	bs1 = ub3.GetSet(id1)
	switch {
	case bs1.Len() != 2:
		t.Fatalf("Incorrect length of set")
	case !bs1.Contains(2):
		t.Fatalf("Set missing element")
	case !bs1.Contains(4):
		t.Fatalf("Set missing element")
	}

	// Difference test
	bs1.Clear()

	ub4 := make(UniqueBag)
	ub4.Add(1, id1)
	ub4.Add(2, id1)
	ub4.Add(5, id2)
	ub4.Add(8, id2)

	ub5 := make(UniqueBag)
	ub5.Add(5, id2)
	ub5.Add(5, id1)

	ub4.Difference(&ub5)

	if len(ub5.List()) != 2 {
		t.Fatalf("Incorrect number of ids in Unique Bag")
	}

	ub4id1 := ub4.GetSet(id1)
	switch {
	case ub4id1.Len() != 2:
		t.Fatalf("Set of Unique Bag has incorrect length")
	case !ub4id1.Contains(1):
		t.Fatalf("Set of Unique Bag missing element")
	case !ub4id1.Contains(2):
		t.Fatalf("Set of Unique Bag missing element")
	}

	ub4id2 := ub4.GetSet(id2)
	if ub4id2.Len() != 1 {
		t.Fatalf("Set of Unique Bag has incorrect length")
	} else if !ub4id2.Contains(8) {
		t.Fatalf("Set of Unique Bag missing element")
	}

	// DifferenceSet test

	ub6 := make(UniqueBag)
	ub6.Add(1, id1)
	ub6.Add(2, id1)
	ub6.Add(7, id1)

	diffBitSet := BitSet(0)
	diffBitSet.Add(1)
	diffBitSet.Add(7)

	ub6.DifferenceSet(id1, diffBitSet)

	ub6id1 := ub6.GetSet(id1)

	if ub6id1.Len() != 1 {
		t.Fatalf("Set of Unique Bag missing element")
	} else if !ub6id1.Contains(2) {
		t.Fatalf("Set of Unique Bag missing element")
	}
}

func TestUniqueBagClear(t *testing.T) {
	b := UniqueBag{}
	id1, id2 := GenerateTestID(), GenerateTestID()
	b.Add(0, id1)
	b.Add(1, id1, id2)

	b.Clear()
	assert.Len(t, b.List(), 0)

	bs := b.GetSet(id1)
	assert.EqualValues(t, 0, bs.Len())

	bs = b.GetSet(id2)
	assert.EqualValues(t, 0, bs.Len())
}
