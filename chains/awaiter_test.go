// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/timer"
)

func TestAwaiter(t *testing.T) {
	vdrID0 := ids.NewShortID([20]byte{0})
	vdrID1 := ids.NewShortID([20]byte{1})
	vdrID2 := ids.NewShortID([20]byte{2})
	vdrID3 := ids.NewShortID([20]byte{3})

	s := validators.NewSet()
	s.Add(validators.NewValidator(vdrID0, 1, time.Now(), timer.MaxTime))
	s.Add(validators.NewValidator(vdrID1, 1, time.Now(), timer.MaxTime))
	s.Add(validators.NewValidator(vdrID3, 1, time.Now(), timer.MaxTime))

	called := make(chan struct{}, 1)
	aw := NewAwaiter(s, 3, func() {
		called <- struct{}{}
	})

	if aw.Connected(vdrID0) {
		t.Fatalf("shouldn't have finished handling yet")
	} else if aw.Connected(vdrID1) {
		t.Fatalf("shouldn't have finished handling yet")
	} else if aw.Disconnected(vdrID1) {
		t.Fatalf("shouldn't have finished handling yet")
	} else if aw.Connected(vdrID1) {
		t.Fatalf("shouldn't have finished handling yet")
	} else if aw.Connected(vdrID2) {
		t.Fatalf("shouldn't have finished handling yet")
	} else if !aw.Connected(vdrID3) {
		t.Fatalf("should have finished handling")
	}

	<-called
}
