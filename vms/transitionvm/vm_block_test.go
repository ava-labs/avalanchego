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
	for _, mode := range verifyModes {
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
	for _, mode := range verifyModes {
		t.Run(mode.String(), func(t *testing.T) {
			sut := newSUT(t)
			ctx := t.Context()

			transitionBlock, err := sut.BuildBlock(ctx)
			require.NoError(t, err)
			require.NoError(t, verifyBlock(ctx, transitionBlock, mode))

			// A child of the transition block sits past the transition time, so
			// it can't be verified as a pre-transition block.
			child, err := sut.BuildBlock(ctx)
			require.NoError(t, err)
			require.ErrorIs(t, verifyBlock(ctx, child, mode), errPostTransitionBlockBeforeTransition)
		})
	}
}

// TestNoTransitionBeforeTime verifies accepting a block before the transition
// time leaves the VM on the pre-transition chain.
func TestNoTransitionBeforeTime(t *testing.T) {
	for _, mode := range verifyModes {
		t.Run(mode.String(), func(t *testing.T) {
			// Two blocks to transition; this test accepts one.
			sut := newSUT(t, withBlocksUntilTransition(2))
			ctx := t.Context()

			sut.BuildVerifyAccept(t, ctx, mode)

			version, err := sut.Version(ctx)
			require.NoError(t, err)
			require.Equal(t, "pre", version)
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
	require.NoError(t, err)
	require.NoError(t, loser.Verify(ctx))
	loserBlock := sut.pre.tip

	sut.pre.tip = genesis
	sut.BuildVerifyAccept(t, ctx, verifyNoContext) // Transition with a block conflicting loser.

	// Rejecting the loser must be a noop, not a fatal error.
	loserBlock.RejectV = errors.New("reject forwarded to shut-down pre-transition chain")
	require.NoError(t, loser.Reject(ctx))
}
