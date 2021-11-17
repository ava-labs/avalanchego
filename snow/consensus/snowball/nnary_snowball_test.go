// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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

	if pref := sb.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Red)

	if pref := sb.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Should be finalized")
	}
}

func TestVirtuousNnarySnowball(t *testing.T) {
	betaVirtuous := 1
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)

	if pref := sb.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Red)

	if pref := sb.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if !sb.Finalized() {
		t.Fatalf("Should be finalized")
	}
}

func TestNarySnowballRecordUnsuccessfulPoll(t *testing.T) {
	betaVirtuous := 2
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
	sb.Add(Blue)

	if pref := sb.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordUnsuccessfulPoll()

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.Preference(); Blue != pref {
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

		if pref := sb.Preference(); Blue != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
		} else if !sb.Finalized() {
			t.Fatalf("Finalized too late")
		}
	}
}

func TestNarySnowballDifferentSnowflakeColor(t *testing.T) {
	betaVirtuous := 2
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
	sb.Add(Blue)

	if pref := sb.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Blue)

	if pref := sb.nnarySnowflake.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	}

	sb.RecordSuccessfulPoll(Red)

	if pref := sb.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if pref := sb.nnarySnowflake.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	}
}
