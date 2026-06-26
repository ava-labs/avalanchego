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
			require.NoError(t, sut.pre.sendAppRequest(ctx, nodeID, responseID))
			require.NoError(t, sut.pre.sendAppRequest(ctx, nodeID, failureID))

			sut.BuildVerifyAccept(t, ctx, verifyNoContext)

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
				require.NoError(t, sut.AppResponse(ctx, nodeID, responseID, nil))
				require.Equal(t, !test.transitions, delivered)
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
				require.NoError(t, sut.AppRequestFailed(ctx, nodeID, failureID, common.ErrUndefined))
				require.Equal(t, !test.transitions, delivered)
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
		require.NoError(t, sut.Connected(ctx, nodeID, v))
	}

	disconnected := ids.GenerateTestNodeID()
	require.NoError(t, sut.Connected(ctx, disconnected, version.Current))
	require.NoError(t, sut.Disconnected(ctx, disconnected))

	sut.BuildVerifyAccept(t, ctx, verifyNoContext) // triggers the transition

	require.Equal(t, want, sut.post.connected)
}
