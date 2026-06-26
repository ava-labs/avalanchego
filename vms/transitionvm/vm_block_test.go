// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransitionBlockChildren(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	// Build and verify the block that reaches the transition time.
	blk, err := sut.BuildBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, blk.Verify(ctx))

	// A child of that block sits past the transition time, so it can't be
	// verified as a pre-transition block.
	child, err := sut.BuildBlock(ctx)
	require.NoError(t, err)
	require.ErrorIs(t, child.Verify(ctx), errPreTransitionBlockAfterTransition)
}

// TestNoTransitionBeforeTime verifies that accepting a block before the
// transition time leaves the VM on its pre-transition chain.
func TestNoTransitionBeforeTime(t *testing.T) {
	// Require two blocks before transitioning; this test only accepts one.
	sut := newSUT(t, withBlocksUntilTransition(2))
	ctx := t.Context()

	sut.BuildVerifyAccept(t, ctx)

	version, err := sut.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "pre", version)
}
