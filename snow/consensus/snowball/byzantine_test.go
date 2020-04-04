// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/prometheus/client_golang/prometheus"
)

func TestByzantine(t *testing.T) {
	params := Parameters{
		Metrics: prometheus.NewRegistry(),
		K:       1, Alpha: 1, BetaVirtuous: 3, BetaRogue: 5,
	}

	byzFactory := ByzantineFactory{}
	byz := byzFactory.New()
	byz.Initialize(params, Blue)

	if ret := byz.Parameters(); ret != params {
		t.Fatalf("Should have returned the correct params")
	}

	byz.Add(Green)

	if pref := byz.Preference(); !pref.Equals(Blue) {
		t.Fatalf("Wrong preference, expected %s returned %s", Blue, pref)
	}

	oneGreen := ids.Bag{}
	oneGreen.Add(Green)
	byz.RecordPoll(oneGreen)

	if pref := byz.Preference(); !pref.Equals(Blue) {
		t.Fatalf("Wrong preference, expected %s returned %s", Blue, pref)
	}

	byz.RecordUnsuccessfulPoll()

	if pref := byz.Preference(); !pref.Equals(Blue) {
		t.Fatalf("Wrong preference, expected %s returned %s", Blue, pref)
	}

	if final := byz.Finalized(); !final {
		t.Fatalf("Should be marked as accepted")
	}

	if str := byz.String(); str != Blue.String() {
		t.Fatalf("Wrong string, expected %s returned %s", Blue, str)
	}
}
