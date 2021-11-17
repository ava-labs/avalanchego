// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mockable

import (
	"testing"
	"time"
)

func TestClockSet(t *testing.T) {
	clock := Clock{}
	clock.Set(time.Unix(1000000, 0))
	if clock.faked == false {
		t.Error("Fake time was set, but .faked flag was not set")
	}
	if !clock.Time().Equal(time.Unix(1000000, 0)) {
		t.Error("Fake time was set, but not returned")
	}
}

func TestClockSync(t *testing.T) {
	clock := Clock{true, time.Unix(0, 0)}
	clock.Sync()
	if clock.faked == true {
		t.Error("Clock was synced, but .faked flag was set")
	}
	if clock.Time().Equal(time.Unix(0, 0)) {
		t.Error("Clock was synced, but returned a fake time")
	}
}

func TestClockUnix(t *testing.T) {
	clock := Clock{true, time.Unix(-14159040, 0)}
	actual := clock.Unix()
	if actual != 0 {
		// We are Unix of 1970s, Moon landings are irrelevant
		t.Errorf("Expected time prior to Unix epoch to be clamped to 0, got %d", actual)
	}
}
