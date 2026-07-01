// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPostTransitionBlock verifies that after the transition, blocks are built
// and accepted by the post-transition chain.
func TestPostTransitionBlock(t *testing.T) {
	for _, mode := range contextModes {
		t.Run(mode.String(), func(t *testing.T) {
			sut := newSUT(t, withBlocksUntilTransition(0))
			ctx := t.Context()

			sut.BuildVerifyAccept(t, ctx, mode) // should be built unwrapped
		})
	}
}

// TestTransitionBlockChildren verifies that a pre-transition block whose parent
// is at or after the transition time fails verification.
func TestTransitionBlockChildren(t *testing.T) {
	for _, mode := range contextModes {
		t.Run(mode.String(), func(t *testing.T) {
			sut := newSUT(t)
			ctx := t.Context()

			transitionBlock, err := sut.BuildBlock(ctx)
			require.NoErrorf(t, err, "%T.BuildBlock()", sut)
			require.NoErrorf(t, verifyBlock(ctx, transitionBlock, mode), "verifyBlock(%T, %s)", transitionBlock, mode)

			// A child of the transition block sits past the transition time, so
			// it can't be verified as a pre-transition block.
			child, err := sut.BuildBlock(ctx)
			require.NoErrorf(t, err, "%T.BuildBlock()", sut)
			require.ErrorIsf(t, verifyBlock(ctx, child, mode), errPostTransitionBlockBeforeTransition, "verifyBlock(%T, %s)", child, mode)
		})
	}
}

// TestNoTransitionBeforeTime verifies accepting a block before the transition
// time leaves the VM on the pre-transition chain.
func TestNoTransitionBeforeTime(t *testing.T) {
	for _, mode := range contextModes {
		t.Run(mode.String(), func(t *testing.T) {
			// Two blocks to transition; this test accepts one.
			sut := newSUT(t, withBlocksUntilTransition(2))
			ctx := t.Context()

			sut.BuildVerifyAccept(t, ctx, mode)

			version, err := sut.Version(ctx)
			require.NoErrorf(t, err, "%T.Version()", sut)
			require.Equalf(t, "pre", version, "%T.Version()", sut)
		})
	}
}

// TestCachedBlockUpdatesAfterTransition verifies that a block which was parsed
// and then cached by the consensus engine before the transition can be
// correctly verified and accepted after the transition.
func TestCachedBlockUpdatesAfterTransition(t *testing.T) {
	for _, mode := range contextModes {
		t.Run(mode.String(), func(t *testing.T) {
			sut := newSUT(t)
			ctx := t.Context()

			transitionBlock, err := sut.BuildBlock(ctx)
			require.NoErrorf(t, err, "%T.BuildBlock()", sut)
			require.NoErrorf(t, verifyBlock(ctx, transitionBlock, mode), "verifyBlock(%T, %s)", transitionBlock, mode)

			// Before accepting the transition block, so before the VM
			// transitions, generate the next block.
			postTransitionBlock, err := sut.BuildBlock(ctx)
			require.NoErrorf(t, err, "%T.BuildBlock()", sut)

			// Even though the block is currently invalid, the consensus engine
			// may cache it.
			require.ErrorIsf(t, verifyBlock(ctx, postTransitionBlock, mode), errPostTransitionBlockBeforeTransition, "verifyBlock(%T, %s)", postTransitionBlock, mode)

			require.NoErrorf(t, transitionBlock.Accept(ctx), "%T.Accept()", transitionBlock)
			version, err := sut.Version(ctx)
			require.NoErrorf(t, err, "%T.Version()", sut)
			require.Equalf(t, "post", version, "%T.Version()", sut)

			require.NoErrorf(t, verifyBlock(ctx, postTransitionBlock, mode), "verifyBlock(%T, %s)", postTransitionBlock, mode)
			require.NoErrorf(t, postTransitionBlock.Accept(ctx), "%T.Accept()", postTransitionBlock)
		})
	}
}

// TestRejectIsNoopAfterTransition verifies rejecting a pre-transition block
// after the transition is a noop.
func TestRejectIsNoopAfterTransition(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	genesis := sut.pre.tip

	loser, err := sut.BuildBlock(ctx)
	require.NoErrorf(t, err, "%T.BuildBlock()", sut)
	require.NoErrorf(t, loser.Verify(ctx), "%T.Verify()", loser)
	loserBlock := sut.pre.tip

	sut.pre.tip = genesis
	sut.BuildVerifyAccept(t, ctx, noContext) // Transition with a block conflicting loser.

	// Rejecting the loser must be a noop, not a fatal error.
	loserBlock.RejectV = errors.New("reject forwarded to shut-down pre-transition chain")
	require.NoErrorf(t, loser.Reject(ctx), "%T.Reject()", loser)
}
