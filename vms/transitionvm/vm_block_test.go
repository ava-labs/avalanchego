// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTransitionBlockChildren verifies that a pre-transition block whose parent
// is at or after the transition time fails verification.
func TestTransitionBlockChildren(t *testing.T) {
	for _, mode := range contextModes {
		t.Run(string(mode), func(t *testing.T) {
			sut := newSUT(t, 1)
			ctx := t.Context()

			transitionBlock, err := sut.BuildBlock(ctx)
			require.NoErrorf(t, err, "%T.BuildBlock()", sut.VM)
			require.NoErrorf(t, verifyBlock(ctx, transitionBlock, mode), "verifyBlock(%T, %s)", transitionBlock, mode)

			// A child of the transition block sits past the transition time, so
			// it can't be verified as a pre-transition block.
			child, err := sut.BuildBlock(ctx)
			require.NoErrorf(t, err, "%T.BuildBlock()", sut.VM)
			require.ErrorIsf(t, verifyBlock(ctx, child, mode), errPostTransitionBlockBeforeTransition, "verifyBlock(%T, %s)", child, mode)
		})
	}
}

// TestCachedBlockUpdatesAfterTransition verifies that a block which was parsed
// and then cached by the consensus engine before the transition can be
// correctly verified and accepted after the transition.
func TestCachedBlockUpdatesAfterTransition(t *testing.T) {
	for _, mode := range contextModes {
		t.Run(string(mode), func(t *testing.T) {
			sut := newSUT(t, 1)
			ctx := t.Context()

			transitionBlock, err := sut.BuildBlock(ctx)
			require.NoErrorf(t, err, "%T.BuildBlock()", sut.VM)
			require.NoErrorf(t, verifyBlock(ctx, transitionBlock, mode), "verifyBlock(%T, %s)", transitionBlock, mode)

			// Before accepting the transition block, so before the VM
			// transitions, generate the next block.
			postTransitionBlock, err := sut.BuildBlock(ctx)
			require.NoErrorf(t, err, "%T.BuildBlock()", sut.VM)

			// Even though the block is currently invalid, the consensus engine
			// may cache it.
			require.ErrorIsf(t, verifyBlock(ctx, postTransitionBlock, mode), errPostTransitionBlockBeforeTransition, "verifyBlock(%T, %s)", postTransitionBlock, mode)

			require.NoErrorf(t, transitionBlock.Accept(ctx), "%T.Accept()", transitionBlock)
			sut.requireVersion(t, "post")

			sut.pre.tip.VerifyV = errors.New("verify forwarded to stale pre-transition block")
			require.NoErrorf(t, verifyBlock(ctx, postTransitionBlock, mode), "verifyBlock(%T, %s)", postTransitionBlock, mode)
			require.NoErrorf(t, postTransitionBlock.Accept(ctx), "%T.Accept()", postTransitionBlock)
		})
	}
}

// TestRejectForwardedToPreTransitionChain verifies rejecting a pre-transition
// block before the transition is forwarded to the pre-transition chain.
func TestRejectForwardedToPreTransitionChain(t *testing.T) {
	// Two blocks to transition, so the built block is pre-transition.
	sut := newSUT(t, 2)
	ctx := t.Context()

	blk, err := sut.BuildBlock(ctx)
	require.NoErrorf(t, err, "%T.BuildBlock()", sut.VM)
	require.NoErrorf(t, blk.Verify(ctx), "%T.Verify()", blk)

	errRejected := errors.New("rejected by pre-transition chain")
	sut.pre.tip.RejectV = errRejected
	require.ErrorIsf(t, blk.Reject(ctx), errRejected, "%T.Reject()", blk)
}

// TestRejectForwardedToPostTransitionChain verifies rejecting a post-transition
// block after the transition is forwarded to the post-transition chain.
func TestRejectForwardedToPostTransitionChain(t *testing.T) {
	sut := newSUT(t, 0)
	ctx := t.Context()

	blk, err := sut.BuildBlock(ctx)
	require.NoErrorf(t, err, "%T.BuildBlock()", sut.VM)
	require.NoErrorf(t, blk.Verify(ctx), "%T.Verify()", blk)

	errRejected := errors.New("rejected by post-transition chain")
	sut.post.tip.RejectV = errRejected
	require.ErrorIsf(t, blk.Reject(ctx), errRejected, "%T.Reject()", blk)
}

// TestRejectIsNoopAfterTransition verifies rejecting a pre-transition block
// after the transition is a noop.
func TestRejectIsNoopAfterTransition(t *testing.T) {
	sut := newSUT(t, 1)
	ctx := t.Context()

	genesis := sut.pre.tip

	loser, err := sut.BuildBlock(ctx)
	require.NoErrorf(t, err, "%T.BuildBlock()", sut.VM)
	require.NoErrorf(t, loser.Verify(ctx), "%T.Verify()", loser)
	loserBlock := sut.pre.tip

	sut.pre.tip = genesis
	sut.BuildVerifyAccept(t, ctx, noContext) // Transition with a block conflicting loser.

	// Rejecting the loser must be a noop, not a fatal error.
	loserBlock.RejectV = errors.New("reject forwarded to shut-down pre-transition chain")
	require.NoErrorf(t, loser.Reject(ctx), "%T.Reject()", loser)
}
