// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// TestTransitionMaintainsState verifies the consensus state set before the
// transition is applied to the post-transition chain.
func TestTransitionMaintainsState(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	require.NoErrorf(t, sut.SetState(ctx, snow.NormalOp), "%T.SetState()", sut)
	require.Equalf(t, snow.NormalOp, sut.pre.consensusState, "%T.consensusState", sut.pre)

	sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition

	require.Equalf(t, snow.NormalOp, sut.post.consensusState, "%T.consensusState", sut.post)
}

// TestWaitForEventForwardsToCurrentChain verifies WaitForEvent routes to the
// current chain, before and after the transition.
func TestWaitForEventForwardsToCurrentChain(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	sut.pre.events <- common.PendingTxs
	msg, err := sut.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", sut)
	require.Equalf(t, common.PendingTxs, msg, "%T.WaitForEvent()", sut)

	sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition

	sut.post.events <- common.StateSyncDone
	msg, err = sut.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", sut)
	require.Equalf(t, common.StateSyncDone, msg, "%T.WaitForEvent()", sut)
}

// TestWaitForEventCanceledByTransition verifies a WaitForEvent call blocked on
// the pre-transition chain is canceled by the transition, so it can take the
// write lock instead of deadlocking.
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

		synctest.Wait()                          // wait until WaitForEvent is durably blocked
		sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition, canceling the wait
		require.ErrorIsf(t, <-errs, context.Canceled, "%T.WaitForEvent()", sut)
	})
}
