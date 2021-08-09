// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"
	"testing"
)

func TestBlockchainStatusValid(t *testing.T) {
	if err := Validating.Valid(); err != nil {
		t.Fatalf("%s failed verification", Validating)
	} else if err := Created.Valid(); err != nil {
		t.Fatalf("%s failed verification", Created)
	} else if err := Preferred.Valid(); err != nil {
		t.Fatalf("%s failed verification", Preferred)
	} else if err := Syncing.Valid(); err != nil {
		t.Fatalf("%s failed verification", Syncing)
	} else if badStatus := BlockchainStatus(math.MaxInt32); badStatus.Valid() == nil {
		t.Fatalf("%s passed verification", badStatus)
	}
}

func TestBlockchainStatusString(t *testing.T) {
	if Validating.String() != "Validating" {
		t.Fatalf("%s failed printing", Validating)
	} else if Created.String() != "Created" {
		t.Fatalf("%s failed printing", Created)
	} else if Preferred.String() != "Preferred" {
		t.Fatalf("%s failed printing", Preferred)
	} else if Syncing.String() != "Syncing" {
		t.Fatalf("%s failed printing", Syncing)
	} else if badStatus := BlockchainStatus(math.MaxInt32); badStatus.String() != "Invalid blockchain status" {
		t.Fatalf("%s failed printing", badStatus)
	}
}
