// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"
)

func TestBinarySnowflake(t *testing.T) {
	Blue := 0
	Red := 1

	beta := 2

	sf := binarySnowflake{}
	sf.Initialize(beta, Red)

	if pref := sf.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Blue)

	if pref := sf.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Red)

	if pref := sf.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Blue)

	if pref := sf.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Blue)

	if pref := sf.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if !sf.Finalized() {
		t.Fatalf("Didn't finalized correctly")
	}
}
