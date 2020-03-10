// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	if bs1.Len() != 2 {
		t.Fatalf("Wrong set length")
	} else if !bs1.Contains(5) {
		t.Fatalf("Set should contain element")
	} else if !bs1.Contains(10) {
		t.Fatalf("Set should contain element")
	}

	bs1.Add(10)
	if bs1.Len() != 2 {
		t.Fatalf("Wrong set length")
	} else if !bs1.Contains(5) {
		t.Fatalf("Set should contain element")
	} else if !bs1.Contains(10) {
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
	if bs1.Len() != 2 {
		t.Fatalf("Wrong set length")
	} else if !bs1.Contains(5) {
		t.Fatalf("Set should contain element")
	} else if !bs1.Contains(10) {
		t.Fatalf("Set should contain element")
	} else if bs2.Len() != 3 {
		t.Fatalf("Wrong set length")
	} else if !bs2.Contains(0) {
		t.Fatalf("Set should contain element")
	} else if !bs2.Contains(5) {
		t.Fatalf("Set should contain element")
	} else if !bs2.Contains(10) {
		t.Fatalf("Set should contain element")
	}

	bs1.Clear()
	if bs1.Len() != 0 {
		t.Fatalf("Wrong set length")
	} else if bs2.Len() != 3 {
		t.Fatalf("Wrong set length")
	} else if !bs2.Contains(0) {
		t.Fatalf("Set should contain element")
	} else if !bs2.Contains(5) {
		t.Fatalf("Set should contain element")
	} else if !bs2.Contains(10) {
		t.Fatalf("Set should contain element")
	}

	bs1.Add(63)
	if bs1.Len() != 1 {
		t.Fatalf("Wrong set length")
	} else if !bs1.Contains(63) {
		t.Fatalf("Set should contain element")
	}

	bs1.Add(1)
	if bs1.Len() != 2 {
		t.Fatalf("Wrong set length")
	} else if !bs1.Contains(1) {
		t.Fatalf("Set should contain element")
	} else if !bs1.Contains(63) {
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

	if bs3.Len() != 2 {
		t.Fatalf("Wrong set length")
	} else if !bs3.Contains(2) {
		t.Fatalf("Set should contain element")
	} else if !bs3.Contains(5) {
		t.Fatalf("Set should contain element")
	} else if bs4.Len() != 2 {
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

	if bs5.Len() != 1 {
		t.Fatalf("Wrong set length")
	} else if !bs5.Contains(7) {
		t.Fatalf("Set should contain element")
	} else if bs6.Len() != 2 {
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
