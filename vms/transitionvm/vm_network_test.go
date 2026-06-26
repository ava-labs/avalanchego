// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// TestPreTransitionRequestRouting verifies that the response to and failure of a
// request the pre-transition chain issued are delivered to it while it remains
// current, but dropped once the VM transitions to the post-transition chain,
// which never issued the request.
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

			// The pre-transition chain issues two requests, one to be answered
			// with a response and one with a failure.
			nodeID := ids.GenerateTestNodeID()
			const (
				responseID = 1
				failureID  = 2
			)
			require.NoError(t, sut.pre.sendAppRequest(ctx, nodeID, responseID))
			require.NoError(t, sut.pre.sendAppRequest(ctx, nodeID, failureID))

			sut.BuildVerifyAccept(t, ctx)

			t.Run("AppResponse", func(t *testing.T) {
				delivered := false
				sut.pre.VM.AppResponseF = func(context.Context, ids.NodeID, uint32, []byte) error {
					delivered = true
					return nil
				}
				sut.post.VM.AppResponseF = func(context.Context, ids.NodeID, uint32, []byte) error {
					require.FailNow(t, "post-transition chain received a pre-transition response")
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
					require.FailNow(t, "post-transition chain received a pre-transition app error")
					return nil
				}
				require.NoError(t, sut.AppRequestFailed(ctx, nodeID, failureID, common.ErrUndefined))
				require.Equal(t, !test.transitions, delivered)
			})
		})
	}
}
