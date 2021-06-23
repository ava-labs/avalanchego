// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestT(t *testing.T) {
	toEngine := make(chan common.Message, 10)
	startTime := time.Now().Add(time.Second)

	s, fromVM := New(toEngine, startTime)
	go s.Dispatch()

	fromVM <- common.PendingTxs

	<-toEngine
	if time.Until(startTime) > 0 {
		t.Fatalf("passed message too soon")
	}
}
