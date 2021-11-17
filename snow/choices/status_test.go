// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package choices

import (
	"math"
	"testing"
)

func TestStatusValid(t *testing.T) {
	if err := Accepted.Valid(); err != nil {
		t.Fatalf("%s failed verification", Accepted)
	} else if err := Rejected.Valid(); err != nil {
		t.Fatalf("%s failed verification", Rejected)
	} else if err := Processing.Valid(); err != nil {
		t.Fatalf("%s failed verification", Processing)
	} else if err := Unknown.Valid(); err != nil {
		t.Fatalf("%s failed verification", Unknown)
	} else if badStatus := Status(math.MaxInt32); badStatus.Valid() == nil {
		t.Fatalf("%s passed verification", badStatus)
	}
}

func TestStatusDecided(t *testing.T) {
	if !Accepted.Decided() {
		t.Fatalf("%s failed decision", Accepted)
	} else if !Rejected.Decided() {
		t.Fatalf("%s failed decision", Rejected)
	} else if Processing.Decided() {
		t.Fatalf("%s failed decision", Processing)
	} else if Unknown.Decided() {
		t.Fatalf("%s failed decision", Unknown)
	} else if badStatus := Status(math.MaxInt32); badStatus.Decided() {
		t.Fatalf("%s failed decision", badStatus)
	}
}

func TestStatusFetched(t *testing.T) {
	if !Accepted.Fetched() {
		t.Fatalf("%s failed issue", Accepted)
	} else if !Rejected.Fetched() {
		t.Fatalf("%s failed issue", Rejected)
	} else if !Processing.Fetched() {
		t.Fatalf("%s failed issue", Processing)
	} else if Unknown.Fetched() {
		t.Fatalf("%s failed issue", Unknown)
	} else if badStatus := Status(math.MaxInt32); badStatus.Fetched() {
		t.Fatalf("%s failed issue", badStatus)
	}
}

func TestStatusString(t *testing.T) {
	if Accepted.String() != "Accepted" {
		t.Fatalf("%s failed printing", Accepted)
	} else if Rejected.String() != "Rejected" {
		t.Fatalf("%s failed printing", Rejected)
	} else if Processing.String() != "Processing" {
		t.Fatalf("%s failed printing", Processing)
	} else if Unknown.String() != "Unknown" {
		t.Fatalf("%s failed printing", Unknown)
	} else if badStatus := Status(math.MaxInt32); badStatus.String() != "Invalid status" {
		t.Fatalf("%s failed printing", badStatus)
	}
}
