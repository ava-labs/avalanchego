// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestDelayFromNew(t *testing.T) {
	toEngine := make(chan common.Message, 10)
	startTime := time.Now().Add(50 * time.Millisecond)

	s, fromVM := New(toEngine, startTime)
	defer s.Close()
	go s.Dispatch()

	fromVM <- common.PendingTxs

	<-toEngine
	if time.Until(startTime) > 0 {
		t.Fatalf("passed message too soon")
	}
}

func TestDelayFromSetTime(t *testing.T) {
	toEngine := make(chan common.Message, 10)
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	s, fromVM := New(toEngine, now)
	defer s.Close()
	go s.Dispatch()

	s.SetStartTime(startTime)

	fromVM <- common.PendingTxs

	<-toEngine
	if time.Until(startTime) > 0 {
		t.Fatalf("passed message too soon")
	}
}

func TestReceipt(t *testing.T) {
	toEngine := make(chan common.Message, 10)
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	s, fromVM := New(toEngine, now)
	defer s.Close()
	go s.Dispatch()

	fromVM <- common.PendingTxs

	s.SetStartTime(startTime)

	<-toEngine
}
