// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import "testing"

func TestBitSet(t *testing.T) {
	var bs1 BitSet

	if bs1.Len() != 0 {
		t.Fatalf("Empty set's len should be 0")
	}

	bs1.Add(5)
	if bs1.Len() != 1 {
		t.Fatalf("Wrong set length")
	} else if !bs1.Contains(5) {
		t.Fatalf("Set should contain element")
	}

	bs1.Add(10)
	switch {
	case bs1.Len() != 2:
		t.Fatalf("Wrong set length")
	case !bs1.Contains(5):
		t.Fatalf("Set should contain element")
	case !bs1.Contains(10):
		t.Fatalf("Set should contain element")
	}

	bs1.Add(10)
	switch {
	case bs1.Len() != 2:
		t.Fatalf("Wrong set length")
	case !bs1.Contains(5):
		t.Fatalf("Set should contain element")
	case !bs1.Contains(10):
		t.Fatalf("Set should contain element")
	}

	var bs2 BitSet

	bs2.Add(0)
	if bs2.Len() != 1 {
		t.Fatalf("Wrong set length")
	} else if !bs2.Contains(0) {
		t.Fatalf("Set should contain element")
	}

	bs2.Union(bs1)
	switch {
	case bs1.Len() != 2:
		t.Fatalf("Wrong set length")
	case !bs1.Contains(5):
		t.Fatalf("Set should contain element")
	case !bs1.Contains(10):
		t.Fatalf("Set should contain element")
	case bs2.Len() != 3:
		t.Fatalf("Wrong set length")
	case !bs2.Contains(0):
		t.Fatalf("Set should contain element")
	case !bs2.Contains(5):
		t.Fatalf("Set should contain element")
	case !bs2.Contains(10):
		t.Fatalf("Set should contain element")
	}

	bs1.Clear()
	switch {
	case bs1.Len() != 0:
		t.Fatalf("Wrong set length")
	case bs2.Len() != 3:
		t.Fatalf("Wrong set length")
	case !bs2.Contains(0):
		t.Fatalf("Set should contain element")
	case !bs2.Contains(5):
		t.Fatalf("Set should contain element")
	case !bs2.Contains(10):
		t.Fatalf("Set should contain element")
	}

	bs1.Add(63)
	if bs1.Len() != 1 {
		t.Fatalf("Wrong set length")
	} else if !bs1.Contains(63) {
		t.Fatalf("Set should contain element")
	}

	bs1.Add(1)
	switch {
	case bs1.Len() != 2:
		t.Fatalf("Wrong set length")
	case !bs1.Contains(1):
		t.Fatalf("Set should contain element")
	case !bs1.Contains(63):
		t.Fatalf("Set should contain element")
	}

	bs1.Remove(63)
	if bs1.Len() != 1 {
		t.Fatalf("Wrong set length")
	} else if !bs1.Contains(1) {
		t.Fatalf("Set should contain element")
	}

	var bs3 BitSet

	bs3.Add(0)
	bs3.Add(2)
	bs3.Add(5)

	var bs4 BitSet

	bs4.Add(2)
	bs4.Add(5)

	bs3.Intersection(bs4)

	switch {
	case bs3.Len() != 2:
		t.Fatalf("Wrong set length")
	case !bs3.Contains(2):
		t.Fatalf("Set should contain element")
	case !bs3.Contains(5):
		t.Fatalf("Set should contain element")
	case bs4.Len() != 2:
		t.Fatalf("Wrong set length")
	}

	var bs5 BitSet

	bs5.Add(7)
	bs5.Add(11)
	bs5.Add(9)

	var bs6 BitSet

	bs6.Add(9)
	bs6.Add(11)

	bs5.Difference(bs6)

	switch {
	case bs5.Len() != 1:
		t.Fatalf("Wrong set length")
	case !bs5.Contains(7):
		t.Fatalf("Set should contain element")
	case bs6.Len() != 2:
		t.Fatalf("Wrong set length")
	}
}

func TestBitSetString(t *testing.T) {
	var bs BitSet

	bs.Add(17)

	expected := "0000000000020000"

	if bsString := bs.String(); bsString != expected {
		t.Fatalf("BitSet.String returned %s expected %s", bsString, expected)
	}
}
