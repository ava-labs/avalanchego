// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestAwaiter(t *testing.T) {
	vdrID0 := ids.NewShortID([20]byte{0})
	vdrID1 := ids.NewShortID([20]byte{1})
	vdrID2 := ids.NewShortID([20]byte{2})
	vdrID3 := ids.NewShortID([20]byte{3})

	s := validators.NewSet()
	s.AddWeight(vdrID0, 1)
	s.AddWeight(vdrID1, 1)
	s.AddWeight(vdrID3, 1)

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
