// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/ava-labs/libevm/event"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}

func TestWaitForAtLeastContextAwareness(t *testing.T) {
	ctx := t.Context()

	var feed event.FeedOf[struct{}]
	sut := NewEventCollector(feed.Subscribe)
	defer func() {
		require.NoErrorf(t, sut.Unsubscribe(), "%T.Unsubscribe()", sut)
	}()

	feed.Send(struct{}{})
	require.NoErrorf(t, sut.WaitForAtLeast(ctx, 1), "%T.WaitForAtLeast(1)", sut)

	ctx, cancel := context.WithCancelCause(ctx)
	want := errors.New("error passed to context.CancelCauseFunc")
	cancel(want)
	require.ErrorIsf(t, sut.WaitForAtLeast(ctx, math.MaxInt), want, "%T.WaitForAtLeast([cancelled context], [math.MaxInt])", sut)
}
