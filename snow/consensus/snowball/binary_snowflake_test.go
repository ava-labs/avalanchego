// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"
)

func TestBinarySnowflake(t *testing.T) {
	blue := 0
	red := 1

	beta := 2

	sf := binarySnowflake{}
	sf.Initialize(beta, red)

	if pref := sf.Preference(); pref != red {
		t.Fatalf("Wrong preference. Expected %d got %d", red, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(blue)

	if pref := sf.Preference(); pref != blue {
		t.Fatalf("Wrong preference. Expected %d got %d", blue, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(red)

	if pref := sf.Preference(); pref != red {
		t.Fatalf("Wrong preference. Expected %d got %d", red, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(blue)

	if pref := sf.Preference(); pref != blue {
		t.Fatalf("Wrong preference. Expected %d got %d", blue, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(blue)

	if pref := sf.Preference(); pref != blue {
		t.Fatalf("Wrong preference. Expected %d got %d", blue, pref)
	} else if !sf.Finalized() {
		t.Fatalf("Didn't finalized correctly")
	}
}
