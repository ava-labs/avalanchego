// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
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
			sut := newSUT(t, withBlocksUntilTransition(blocksUntilTransition))
			ctx := t.Context()

			// Issue two requests: one answered with a response, one with a failure.
			nodeID := ids.GenerateTestNodeID()
			const (
				responseID = 1
				failureID  = 2
			)
			require.NoErrorf(t, sut.pre.sendAppRequest(ctx, nodeID, responseID), "%T.sendAppRequest()", sut.pre)
			require.NoErrorf(t, sut.pre.sendAppRequest(ctx, nodeID, failureID), "%T.sendAppRequest()", sut.pre)

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
				require.NoErrorf(t, sut.AppResponse(ctx, nodeID, responseID, nil), "%T.AppResponse()", sut)
				require.Equalf(t, !test.transitions, delivered, "%T.AppResponse()", sut)
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
				require.NoErrorf(t, sut.AppRequestFailed(ctx, nodeID, failureID, common.ErrUndefined), "%T.AppRequestFailed()", sut)
				require.Equalf(t, !test.transitions, delivered, "%T.AppRequestFailed()", sut)
			})
		})
	}
}

// TestTransitionForwardsConnections verifies the transition replays the
// pre-transition chain's connections to the post-transition chain.
func TestTransitionForwardsConnections(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	want := map[ids.NodeID]*version.Application{
		ids.GenerateTestNodeID(): {Name: "avalanchego", Major: 1, Minor: 2, Patch: 3},
		ids.GenerateTestNodeID(): {Name: "avalanchego", Major: 4, Minor: 5, Patch: 6},
	}
	for nodeID, v := range want {
		require.NoErrorf(t, sut.Connected(ctx, nodeID, v), "%T.Connected()", sut)
	}

	disconnected := ids.GenerateTestNodeID()
	require.NoErrorf(t, sut.Connected(ctx, disconnected, version.Current), "%T.Connected()", sut)
	require.NoErrorf(t, sut.Disconnected(ctx, disconnected), "%T.Disconnected()", sut)

	sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition

	require.Equalf(t, want, sut.post.connected, "%T.post.connected", sut)
}
