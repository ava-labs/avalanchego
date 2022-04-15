// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"testing"
	"time"

	"github.com/chain4travel/caminogo/snow/engine/common"
	"github.com/chain4travel/caminogo/utils/logging"
)

func TestDelayFromNew(t *testing.T) {
	toEngine := make(chan common.Message, 10)
	startTime := time.Now().Add(50 * time.Millisecond)

	s, fromVM := New(logging.NoLog{}, toEngine)
	defer s.Close()
	go s.Dispatch(startTime)

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

	s, fromVM := New(logging.NoLog{}, toEngine)
	defer s.Close()
	go s.Dispatch(now)

	s.SetBuildBlockTime(startTime)

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

	s, fromVM := New(logging.NoLog{}, toEngine)
	defer s.Close()
	go s.Dispatch(now)

	fromVM <- common.PendingTxs

	s.SetBuildBlockTime(startTime)

	<-toEngine
}
