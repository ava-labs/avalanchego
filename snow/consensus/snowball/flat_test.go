// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestFlatParams(t *testing.T) { ParamsTest(t, FlatFactory{}) }

func TestFlat(t *testing.T) {
	params := Parameters{
		K: 2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2,
	}
	f := Flat{}
	f.Initialize(params, Red)
	f.Add(Green)
	f.Add(Blue)

	if pref := f.Preference(); pref != Red {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if f.Finalized() {
		t.Fatalf("Finalized too early")
	}

	twoBlue := ids.Bag{}
	twoBlue.Add(Blue, Blue)
	f.RecordPoll(twoBlue)

	if pref := f.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if f.Finalized() {
		t.Fatalf("Finalized too early")
	}

	oneRedOneBlue := ids.Bag{}
	twoBlue.Add(Red, Blue)
	f.RecordPoll(oneRedOneBlue)

	if pref := f.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if f.Finalized() {
		t.Fatalf("Finalized too early")
	}

	f.RecordPoll(twoBlue)

	if pref := f.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if f.Finalized() {
		t.Fatalf("Finalized too early")
	}

	f.RecordPoll(twoBlue)

	if pref := f.Preference(); pref != Blue {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if !f.Finalized() {
		t.Fatalf("Finalized too late")
	}

	expected := "SB(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES, NumSuccessfulPolls = 3, SF(Confidence = 2, Finalized = true, SL(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES)))"
	if str := f.String(); str != expected {
		t.Fatalf("Wrong state. Expected:\n%s\nGot:\n%s", expected, str)
	}
}
