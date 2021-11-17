// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"
)

func TestNnarySnowflake(t *testing.T) {
	betaVirtuous := 2
	betaRogue := 2

	sf := nnarySnowflake{}
	sf.Initialize(betaVirtuous, betaRogue, Red)
	sf.Add(Blue)
	sf.Add(Green)

	if pref := sf.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Blue)

	if pref := sf.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Red)

	if pref := sf.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sf.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sf.RecordSuccessfulPoll(Red)

	if pref := sf.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if !sf.Finalized() {
		t.Fatalf("Should be finalized")
	}

	sf.RecordSuccessfulPoll(Blue)

	if pref := sf.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if !sf.Finalized() {
		t.Fatalf("Should be finalized")
	}
}

func TestVirtuousNnarySnowflake(t *testing.T) {
	betaVirtuous := 2
	betaRogue := 3

	sb := nnarySnowflake{}
	sb.Initialize(betaVirtuous, betaRogue, Red)

	if pref := sb.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Red)

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

func TestRogueNnarySnowflake(t *testing.T) {
	betaVirtuous := 1
	betaRogue := 2

	sb := nnarySnowflake{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
	if sb.rogue {
		t.Fatalf("Shouldn't be rogue")
	}

	sb.Add(Red)
	if sb.rogue {
		t.Fatalf("Shouldn't be rogue")
	}

	sb.Add(Blue)
	if !sb.rogue {
		t.Fatalf("Should be rogue")
	}

	sb.Add(Red)
	if !sb.rogue {
		t.Fatalf("Should be rogue")
	}

	if pref := sb.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if sb.Finalized() {
		t.Fatalf("Finalized too early")
	}

	sb.RecordSuccessfulPoll(Red)

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
