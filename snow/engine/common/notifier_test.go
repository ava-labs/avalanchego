// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common/commontest"
	"github.com/ava-labs/avalanchego/utils/logging"

	. "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestNotifier(t *testing.T) {
	s := commontest.NewSubscriber()
	n := make(chan Message)
	nf := NewNotificationForwarder(&logging.NoLog{}, s, n)
	nf.CheckForEvent()

	select {
	case <-n:
		require.FailNow(t, "unexpected message")

	// TODO: Replace this racy check with the synctest package once the minimum
	// go version is >= 1.25
	case <-time.After(time.Millisecond):
	}

	s.SetEvent(PendingTxs)
	select {
	case msg := <-n:
		require.Equal(t, PendingTxs, msg)

	// TODO: Replace this racy check with the synctest package once the minimum
	// go version is >= 1.25
	case <-time.After(time.Millisecond):
		require.FailNow(t, "expected message")
	}

	s.SetEvent(0)
	nf.CheckForEvent()

	select {
	case <-n:
		require.FailNow(t, "unexpected message")

	// TODO: Replace this racy check with the synctest package once the minimum
	// go version is >= 1.25
	case <-time.After(time.Millisecond):
	}

	nf.Close() // Must not block on n being read
}

func TestNotifierStopWhileSubscribing(_ *testing.T) {
	s := commontest.NewSubscriber()
	n := make(chan Message)
	nf := NewNotificationForwarder(&logging.NoLog{}, s, n)
	nf.CheckForEvent()

	// TODO: Replace this racy check with the synctest package once the minimum
	// go version is >= 1.25
	time.Sleep(time.Millisecond)

	nf.Close() // Must not block on n being read
}

func TestNotifierStopWhileNotifying(*testing.T) {
	s := commontest.NewSubscriber()
	s.SetEvent(PendingTxs)

	n := make(chan Message)
	nf := NewNotificationForwarder(&logging.NoLog{}, s, n)
	nf.CheckForEvent()

	// TODO: Replace this racy check with the synctest package once the minimum
	// go version is >= 1.25
	time.Sleep(time.Millisecond)

	nf.Close() // Must not block on n being read
}
