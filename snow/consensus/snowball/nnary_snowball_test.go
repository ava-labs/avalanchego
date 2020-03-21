// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"
)

func TestNnarySnowball(t *testing.T) {
	betaVirtuous := 2
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
	sb.Add(Blue)
	sb.Add(Green)

	if pref := sb.Preference(); !Red.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Red)

	if pref := sb.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Should be finalized")
	}
}

func TestNnarySnowflake(t *testing.T) {
	betaVirtuous := 2
	betaRogue := 2

	sf := nnarySnowflake{}
	sf.Initialize(betaVirtuous, betaRogue, Red)
	sf.Add(Blue)
	sf.Add(Green)

	if pref := sf.Preference(); !Red.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Blue)

	if pref := sf.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Red)

	if pref := sf.Preference(); !Red.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Red)

	if pref := sf.Preference(); !Red.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if !sf.Finalized() {
		t.Fatalf("Should be finalized")
	}

	sf.RecordSuccessfulPoll(Blue)

	if pref := sf.Preference(); !Red.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if !sf.Finalized() {
		t.Fatalf("Should be finalized")
	}
}

func TestNarySnowballRecordUnsuccessfulPoll(t *testing.T) {
	betaVirtuous := 2
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
	sb.Add(Blue)

	if pref := sb.Preference(); !Red.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordUnsuccessfulPoll()

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Finalized too late")
	}

	expected := "SB(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES, NumSuccessfulPolls = 3, SF(Confidence = 2, Finalized = true, SL(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES)))"
	if str := sb.String(); str != expected {
		t.Fatalf("Wrong state. Expected:\n%s\nGot:\n%s", expected, str)
	}

	for i := 0; i < 4; i++ {
		sb.RecordSuccessfulPoll(Red)

		if pref := sb.Preference(); !Blue.Equals(pref) {
			t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
		} else if !sb.Finalized() {
			t.Fatalf("Finalized too late")
		}
	}
}

func TestNarySnowflakeColor(t *testing.T) {
	betaVirtuous := 2
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
	sb.Add(Blue)

	if pref := sb.Preference(); !Red.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.nnarySnowflake.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	}

	sb.RecordSuccessfulPoll(Red)

	if pref := sb.Preference(); !Blue.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if pref := sb.nnarySnowflake.Preference(); !Red.Equals(pref) {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	}
}
