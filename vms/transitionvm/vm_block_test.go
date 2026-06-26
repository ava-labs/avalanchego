// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransitionBlockChildren(t *testing.T) {
	for _, mode := range verifyModes {
		t.Run(mode.String(), func(t *testing.T) {
			sut := newSUT(t)
			ctx := t.Context()

			// Build and verify the block that reaches the transition time.
			blk, err := sut.BuildBlock(ctx)
			require.NoError(t, err)
			require.NoError(t, verifyBlock(ctx, blk, mode))

			// A child of that block sits past the transition time, so it can't
			// be verified as a pre-transition block.
			child, err := sut.BuildBlock(ctx)
			require.NoError(t, err)
			require.ErrorIs(t, verifyBlock(ctx, child, mode), errPreTransitionBlockAfterTransition)
		})
	}
}

// TestNoTransitionBeforeTime verifies that accepting a block before the
// transition time leaves the VM on its pre-transition chain.
func TestNoTransitionBeforeTime(t *testing.T) {
	for _, mode := range verifyModes {
		t.Run(mode.String(), func(t *testing.T) {
			// Require two blocks before transitioning; this test only accepts one.
			sut := newSUT(t, withBlocksUntilTransition(2))
			ctx := t.Context()

			blk, err := sut.BuildBlock(ctx)
			require.NoError(t, err)
			require.NoError(t, verifyBlock(ctx, blk, mode))
			require.NoError(t, blk.Accept(ctx))

			version, err := sut.Version(ctx)
			require.NoError(t, err)
			require.Equal(t, "pre", version)
		})
	}
}

// TestRejectIsNoopAfterTransition verifies that rejecting a pre-transition block
// after the transition is a noop. The engine may have verified blocks that
// conflict with the transition-triggering block; once that block is accepted the
// pre-transition chain shuts down, so forwarding a later Reject into it could
// error and be treated as fatal. Reject must not forward in that case.
func TestRejectIsNoopAfterTransition(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	genesis := sut.pre.tip

	loser, err := sut.BuildBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, loser.Verify(ctx))
	loserBlock := sut.pre.tip

	sut.pre.tip = genesis
	sut.BuildVerifyAccept(t, ctx) // Trigger the transition with a block that conflicts loser.

	// Rejecting the loser must be a noop, not a fatal error.
	loserBlock.RejectV = errors.New("reject forwarded to shut-down pre-transition chain")
	require.NoError(t, loser.Reject(ctx))
}
