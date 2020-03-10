// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"
	"testing"
)

func TestStatusValid(t *testing.T) {
	if err := Validating.Valid(); err != nil {
		t.Fatalf("%s failed verification", Validating)
	} else if err := Created.Valid(); err != nil {
		t.Fatalf("%s failed verification", Created)
	} else if err := Preferred.Valid(); err != nil {
		t.Fatalf("%s failed verification", Preferred)
	} else if err := Unknown.Valid(); err != nil {
		t.Fatalf("%s failed verification", Unknown)
	} else if badStatus := Status(math.MaxInt32); badStatus.Valid() == nil {
		t.Fatalf("%s passed verification", badStatus)
	}
}

func TestStatusString(t *testing.T) {
	if Validating.String() != "Validating" {
		t.Fatalf("%s failed printing", Validating)
	} else if Created.String() != "Created" {
		t.Fatalf("%s failed printing", Created)
	} else if Preferred.String() != "Preferred" {
		t.Fatalf("%s failed printing", Preferred)
	} else if Unknown.String() != "Unknown" {
		t.Fatalf("%s failed printing", Unknown)
	} else if badStatus := Status(math.MaxInt32); badStatus.String() != "Invalid status" {
		t.Fatalf("%s failed printing", badStatus)
	}
}
