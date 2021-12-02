// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"testing"
)

var errCalculatedSubsetWrong = errors.New("incorrectly calculated whether one duration was subset of other")

func TestValidatorBoundedBy(t *testing.T) {
	// case 1: a starts, a finishes, b starts, b finishes
	aStartTime := uint64(0)
	aEndTIme := uint64(1)
	a := &Validator{
		NodeID: keys[0].PublicKey().Address(),
		Start:  aStartTime,
		End:    aEndTIme,
		Wght:   defaultWeight,
	}

	bStartTime := uint64(2)
	bEndTime := uint64(3)
	b := &Validator{
		NodeID: keys[0].PublicKey().Address(),
		Start:  bStartTime,
		End:    bEndTime,
		Wght:   defaultWeight,
	}

	if a.BoundedBy(b.StartTime(), b.EndTime()) || b.BoundedBy(a.StartTime(), a.EndTime()) {
		t.Fatal(errCalculatedSubsetWrong)
	}

	// case 2: a starts, b starts, a finishes, b finishes
	a.Start = 0
	b.Start = 1
	a.End = 2
	b.End = 3
	if a.BoundedBy(b.StartTime(), b.EndTime()) || b.BoundedBy(a.StartTime(), a.EndTime()) {
		t.Fatal(errCalculatedSubsetWrong)
	}

	// case 3: a starts, b starts, b finishes, a finishes
	a.Start = 0
	b.Start = 1
	b.End = 2
	a.End = 3
	if a.BoundedBy(b.StartTime(), b.EndTime()) || !b.BoundedBy(a.StartTime(), a.EndTime()) {
		t.Fatal(errCalculatedSubsetWrong)
	}

	// case 4: b starts, a starts, a finishes, b finishes
	b.Start = 0
	a.Start = 1
	a.End = 2
	b.End = 3
	if !a.BoundedBy(b.StartTime(), b.EndTime()) || b.BoundedBy(a.StartTime(), a.EndTime()) {
		t.Fatal(errCalculatedSubsetWrong)
	}

	// case 5: b starts, b finishes, a starts, a finishes
	b.Start = 0
	b.End = 1
	a.Start = 2
	a.End = 3
	if a.BoundedBy(b.StartTime(), b.EndTime()) || b.BoundedBy(a.StartTime(), a.EndTime()) {
		t.Fatal(errCalculatedSubsetWrong)
	}

	// case 6: b starts, a starts, b finishes, a finishes
	b.Start = 0
	a.Start = 1
	b.End = 2
	a.End = 3
	if a.BoundedBy(b.StartTime(), b.EndTime()) || b.BoundedBy(a.StartTime(), a.EndTime()) {
		t.Fatal(errCalculatedSubsetWrong)
	}

	// case 3: a starts, b starts, b finishes, a finishes
	a.Start = 0
	b.Start = 0
	b.End = 1
	a.End = 1
	if !a.BoundedBy(b.StartTime(), b.EndTime()) || !b.BoundedBy(a.StartTime(), a.EndTime()) {
		t.Fatal(errCalculatedSubsetWrong)
	}
}
