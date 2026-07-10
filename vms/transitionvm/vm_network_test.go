// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

// TestPreTransitionRequestRouting verifies a response or failure for a request
// the pre-transition chain issued is delivered while it is current, but dropped
// after the transition.
func TestPreTransitionRequestRouting(t *testing.T) {
	tests := []struct {
		name        string
		transitions bool
	}{
		{
			name:        "delivered_without_transition",
			transitions: false,
		},
		{
			name:        "dropped_after_transition",
			transitions: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			blocksUntilTransition := 1
			if !test.transitions {
				blocksUntilTransition = 2
			}
			sut := newSUT(t, blocksUntilTransition)
			ctx := t.Context()

			// Issue two requests: one answered with a response, one with a failure.
			nodeID := ids.GenerateTestNodeID()
			const (
				responseID = 1
				failureID  = 2
			)
			require.NoErrorf(t, sut.pre.appSender.SendAppRequest(ctx, set.Of(nodeID), responseID, nil), "pre-transition %T.SendAppRequest()", sut.pre.appSender)
			require.NoErrorf(t, sut.pre.appSender.SendAppRequest(ctx, set.Of(nodeID), failureID, nil), "pre-transition %T.SendAppRequest()", sut.pre.appSender)

			sut.BuildVerifyAccept(t, ctx, noContext)

			t.Run("AppResponse", func(t *testing.T) {
				delivered := false
				sut.pre.VM.AppResponseF = func(context.Context, ids.NodeID, uint32, []byte) error {
					delivered = true
					return nil
				}
				sut.post.VM.AppResponseF = func(context.Context, ids.NodeID, uint32, []byte) error {
					t.Fatal("post-transition chain received a pre-transition response")
					return nil
				}
				require.NoErrorf(t, sut.AppResponse(ctx, nodeID, responseID, nil), "%T.AppResponse()", sut.VM)
				require.Equalf(t, !test.transitions, delivered, "%T.AppResponse()", sut.VM)
			})

			t.Run("AppRequestFailed", func(t *testing.T) {
				delivered := false
				sut.pre.VM.AppRequestFailedF = func(context.Context, ids.NodeID, uint32, *common.AppError) error {
					delivered = true
					return nil
				}
				sut.post.VM.AppRequestFailedF = func(context.Context, ids.NodeID, uint32, *common.AppError) error {
					t.Fatal("post-transition chain received a pre-transition app error")
					return nil
				}
				require.NoErrorf(t, sut.AppRequestFailed(ctx, nodeID, failureID, common.ErrUndefined), "%T.AppRequestFailed()", sut.VM)
				require.Equalf(t, !test.transitions, delivered, "%T.AppRequestFailed()", sut.VM)
			})
		})
	}
}

// TestRequestIDCollisionAcrossTransition verifies that when the
// post-transition chain reuses a request ID the pre-transition chain sent, the
// first response is delivered to the post-transition chain and the second is
// dropped.
func TestRequestIDCollisionAcrossTransition(t *testing.T) {
	sut := newSUT(t, 1)
	ctx := t.Context()

	nodeID := ids.GenerateTestNodeID()
	const requestID = 1

	// The pre-transition chain's request is still unanswered at the
	// transition.
	require.NoErrorf(t, sut.pre.appSender.SendAppRequest(ctx, set.Of(nodeID), requestID, nil), "pre-transition %T.SendAppRequest()", sut.pre.appSender)
	sut.BuildVerifyAccept(t, ctx, noContext)
	require.NoErrorf(t, sut.post.appSender.SendAppRequest(ctx, set.Of(nodeID), requestID, nil), "post-transition %T.SendAppRequest()", sut.post.appSender)

	delivered := 0
	sut.post.VM.AppResponseF = func(context.Context, ids.NodeID, uint32, []byte) error {
		delivered++
		return nil
	}
	sut.pre.VM.AppResponseF = func(context.Context, ids.NodeID, uint32, []byte) error {
		t.Fatal("pre-transition chain received a response after the transition")
		return nil
	}
	for range 2 {
		require.NoErrorf(t, sut.AppResponse(ctx, nodeID, requestID, nil), "%T.AppResponse()", sut.VM)
		require.Equalf(t, 1, delivered, "%T.AppResponse()", sut.VM)
	}
}

// TestTransitionForwardsConnections verifies the transition replays the
// pre-transition chain's connections to the post-transition chain.
func TestTransitionForwardsConnections(t *testing.T) {
	sut := newSUT(t, 1)
	ctx := t.Context()

	want := map[ids.NodeID]*version.Application{
		ids.GenerateTestNodeID(): {Name: "avalanchego", Major: 1, Minor: 2, Patch: 3},
		ids.GenerateTestNodeID(): {Name: "avalanchego", Major: 4, Minor: 5, Patch: 6},
	}
	for nodeID, v := range want {
		require.NoErrorf(t, sut.Connected(ctx, nodeID, v), "%T.Connected()", sut.VM)
	}

	disconnected := ids.GenerateTestNodeID()
	require.NoErrorf(t, sut.Connected(ctx, disconnected, version.Current), "%T.Connected()", sut.VM)
	require.NoErrorf(t, sut.Disconnected(ctx, disconnected), "%T.Disconnected()", sut.VM)

	sut.BuildVerifyAccept(t, ctx, noContext)

	require.Equalf(t, want, sut.post.connected, "%T.post.connected", sut.VM)
}

// TestAppGossipCanGrabCtxLock verifies that the pre-transition VM can grab its
// ctx.Lock during AppGossip.
func TestAppGossipCanGrabCtxLock(t *testing.T) {
	tests := []struct {
		name                  string
		blocksUntilTransition int
	}{
		{
			name:                  "with_transition",
			blocksUntilTransition: 1,
		},
		{
			name:                  "without_transition",
			blocksUntilTransition: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				sut := newSUT(t, tt.blocksUntilTransition)
				ctx := t.Context()

				sut.pre.VM.AppGossipF = func(context.Context, ids.NodeID, []byte) error {
					sut.pre.chainCtx.Lock.Lock()
					defer sut.pre.chainCtx.Lock.Unlock()

					return nil // Coreth verifies txs here
				}

				sut.ctx.Lock.Lock() // Taken by the consensus thread

				done := make(chan struct{})
				go func() {
					assert.NoError(t, sut.AppGossip(ctx, ids.GenerateTestNodeID(), nil), "%T.AppGossip()", sut.VM)
					close(done)
				}()
				synctest.Wait() // pre-transition gossip is blocked

				// On the consensus thread, the transition lock is grabbed
				sut.BuildVerifyAccept(t, ctx, noContext)

				sut.ctx.Lock.Unlock()
				<-done // pre-transition gossip should have returned
			})
		})
	}
}
