// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"
)

func TestBinarySnowball(t *testing.T) {
	Red := 0
	Blue := 1

	beta := 2

	sb := binarySnowball{}
	sb.Initialize(beta, Red)

	if pref := sb.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Red)

	if pref := sb.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Didn't finalized correctly")
	}
}

func TestBinarySnowballRecordUnsuccessfulPoll(t *testing.T) {
	Red := 0
	Blue := 1

	beta := 2

	sb := binarySnowball{}
	sb.Initialize(beta, Red)

	if pref := sb.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordUnsuccessfulPoll()

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Finalized too late")
	}

	expected := "SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 3, SF(Confidence = 2, Finalized = true, SL(Preference = 1)))"
	if str := sb.String(); str != expected {
		t.Fatalf("Wrong state. Expected:\n%s\nGot:\n%s", expected, str)
	}
}

func TestBinarySnowballAcceptWeirdColor(t *testing.T) {
	Blue := 0
	Red := 1

	beta := 2

	sb := binarySnowball{}
	sb.Initialize(beta, Red)

	if pref := sb.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Red)
	sb.RecordUnsuccessfulPoll()

	if pref := sb.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Red)
	sb.RecordUnsuccessfulPoll()

	if pref := sb.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %d got %d", Blue, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Finalized too late")
	}

	expected := "SB(Preference = 1, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 2, SF(Confidence = 2, Finalized = true, SL(Preference = 0)))"
	if str := sb.String(); str != expected {
		t.Fatalf("Wrong state. Expected:\n%s\nGot:\n%s", expected, str)
	}
}

func TestBinarySnowballLockColor(t *testing.T) {
	Red := 0
	Blue := 1

	beta := 1

	sb := binarySnowball{}
	sb.Initialize(beta, Red)

	sb.RecordSuccessfulPoll(Red)

	if pref := sb.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Finalized too late")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Finalized too late")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %d got %d", Red, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Finalized too late")
	}

	expected := "SB(Preference = 1, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 2, SF(Confidence = 1, Finalized = true, SL(Preference = 0)))"
	if str := sb.String(); str != expected {
		t.Fatalf("Wrong state. Expected:\n%s\nGot:\n%s", expected, str)
	}
}
