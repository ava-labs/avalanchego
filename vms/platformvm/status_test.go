// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"
	"testing"
)

func TestStatusValid(t *testing.T) {
	if err := Committed.Valid(); err != nil {
		t.Fatalf("%s failed verification", Committed)
	} else if err := Aborted.Valid(); err != nil {
		t.Fatalf("%s failed verification", Aborted)
	} else if err := Processing.Valid(); err != nil {
		t.Fatalf("%s failed verification", Processing)
	} else if err := Unknown.Valid(); err != nil {
		t.Fatalf("%s failed verification", Unknown)
	} else if err := Dropped.Valid(); err != nil {
		t.Fatalf("%s failed verification", Dropped)
	} else if badStatus := Status(math.MaxInt32); badStatus.Valid() == nil {
		t.Fatalf("%s passed verification", badStatus)
	}
}

func TestStatusString(t *testing.T) {
	if Committed.String() != "Committed" {
		t.Fatalf("%s failed printing", Committed)
	} else if Aborted.String() != "Aborted" {
		t.Fatalf("%s failed printing", Aborted)
	} else if Processing.String() != "Processing" {
		t.Fatalf("%s failed printing", Processing)
	} else if Unknown.String() != "Unknown" {
		t.Fatalf("%s failed printing", Unknown)
	} else if Dropped.String() != "Dropped" {
		t.Fatalf("%s failed printing", Dropped)
	} else if badStatus := Status(math.MaxInt32); badStatus.String() != "Invalid status" {
		t.Fatalf("%s failed printing", badStatus)
	}
}
