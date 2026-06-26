// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// TestWaitForEventForwardsToCurrentChain verifies that WaitForEvent routes to
// whichever chain is current, before and after the transition.
func TestWaitForEventForwardsToCurrentChain(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	sut.pre.events <- common.PendingTxs
	msg, err := sut.WaitForEvent(ctx)
	require.NoError(t, err)
	require.Equal(t, common.PendingTxs, msg)

	sut.BuildVerifyAccept(t, ctx) // triggers the transition

	sut.post.events <- common.StateSyncDone
	msg, err = sut.WaitForEvent(ctx)
	require.NoError(t, err)
	require.Equal(t, common.StateSyncDone, msg)
}

// TestWaitForEventCanceledByTransition verifies that a WaitForEvent call blocked
// on the pre-transition chain is canceled when the VM transitions, so the
// transition can take the write lock instead of deadlocking against the reader.
func TestWaitForEventCanceledByTransition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sut := newSUT(t)
		ctx := t.Context()

		// pre.events is left empty, so WaitForEvent blocks.
		errs := make(chan error, 1)
		go func() {
			_, err := sut.WaitForEvent(ctx)
			errs <- err
		}()

		synctest.Wait()               // wait until WaitForEvent is durably blocked
		sut.BuildVerifyAccept(t, ctx) // triggers the transition, canceling the wait
		require.ErrorIs(t, <-errs, context.Canceled)
	})
}
