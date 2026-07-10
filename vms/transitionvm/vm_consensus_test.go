// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"math"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// TestTransitionMaintainsState verifies the consensus state set before the
// transition is applied to the post-transition chain.
func TestTransitionMaintainsState(t *testing.T) {
	sut := newSUT(t, 1)
	ctx := t.Context()

	require.NoErrorf(t, sut.SetState(ctx, snow.NormalOp), "%T.SetState()", sut.VM)
	require.Equalf(t, snow.NormalOp, sut.pre.consensusState, "pre-transition %T.consensusState", sut.pre)

	sut.BuildVerifyAccept(t, ctx, noContext)

	require.Equalf(t, snow.NormalOp, sut.post.consensusState, "post-transition %T.consensusState", sut.post)
}

// TestTransitionSkipsInitializingState verifies the transition doesn't forward
// the consensus state if the state was never set.
func TestTransitionSkipsInitializingState(t *testing.T) {
	sut := newSUT(t, 1)
	ctx := t.Context()

	// A forwarded SetState would overwrite this sentinel.
	const unset snow.State = math.MaxUint8
	sut.post.consensusState = unset

	sut.BuildVerifyAccept(t, ctx, noContext)

	require.Equalf(t, unset, sut.post.consensusState, "post-transition %T.consensusState", sut.post)
}

// TestWaitForEventForwardsToCurrentChain verifies WaitForEvent routes to the
// current chain, before and after the transition.
func TestWaitForEventForwardsToCurrentChain(t *testing.T) {
	sut := newSUT(t, 1)
	ctx := t.Context()

	const (
		preEvent common.Message = iota
		postEvent
	)
	sut.pre.events <- preEvent
	sut.post.events <- postEvent

	msg, err := sut.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", sut.VM)
	require.Equalf(t, preEvent, msg, "%T.WaitForEvent()", sut.VM)

	sut.BuildVerifyAccept(t, ctx, noContext)

	msg, err = sut.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", sut.VM)
	require.Equalf(t, postEvent, msg, "%T.WaitForEvent()", sut.VM)
}

// TestWaitForEventCanceledByTransition verifies a WaitForEvent call blocked on
// the pre-transition chain is canceled by the transition, so it can take the
// write lock instead of deadlocking.
func TestWaitForEventCanceledByTransition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sut := newSUT(t, 1)
		ctx := t.Context()

		// pre.events is left empty, so WaitForEvent blocks.
		errs := make(chan error, 1)
		go func() {
			_, err := sut.WaitForEvent(ctx)
			errs <- err
		}()

		synctest.Wait()                          // wait until WaitForEvent is durably blocked
		sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition, canceling the wait
		require.ErrorIsf(t, <-errs, context.Canceled, "%T.WaitForEvent()", sut.VM)
	})
}
