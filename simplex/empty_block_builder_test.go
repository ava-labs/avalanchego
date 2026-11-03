// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"

	"github.com/stretchr/testify/require"
)

// blocksUntilDone mimics the production ShouldBuildEmptyBlock contract
// (Epoch.shouldBuildEmptyBlock): it blocks until the given context is
// cancelled and then reports whether the cancellation was because the empty-block
// timeout elapsed.
func blocksUntilDone(ctx context.Context) bool {
	<-ctx.Done()
	return errors.Is(context.Cause(ctx), common.ErrShouldBuildEmptyBlock)
}

// waitsForCancel blocks until the context is cancelled and never asks for an empty block.
func waitsForCancel(ctx context.Context) bool {
	<-ctx.Done()
	return false
}

// TestEmptyBlockBuilderReturnsRealBlockWhenBuilt verifies that when the underlying
// builder produces a block, that block is returned and the metadata is forwarded.
func TestEmptyBlockBuilderReturnsRealBlockWhenBuilt(t *testing.T) {
	bb := testutil.NewTestControlledBlockBuilder(t)
	md := common.ProtocolMetadata{Version: 1, Epoch: 2, Round: 3, Seq: 4}

	ebb := &simplex.EmptyBlockBuilder{
		// A long timeout guarantees the empty-block path never fires in this test.
		Timeout:               time.Hour,
		BB:                    bb,
		ShouldBuildEmptyBlock: waitsForCancel,
	}

	bb.TriggerNewBlock()
	block, ok := ebb.BuildBlock(context.Background(), md, common.NewBlacklist(5, uint64(5), simplex.DefaultMaxBlacklistedRounds))
	require.True(t, ok)
	require.NotNil(t, block)
	require.Equal(t, md.Seq, block.BlockHeader().Seq)
	require.Equal(t, md.Epoch, block.BlockHeader().Epoch)
}

// TestEmptyBlockBuilderReturnsEmptyBlockWhenShouldBuildEmpty verifies the core wiring:
// when ShouldBuildEmptyBlock fires (because the empty-block timeout elapsed), the
// context handed to the underlying builder is cancelled with ErrShouldBuildEmptyBlock
// as its cause, so the builder returns an empty block and true (per the BlockBuilder
// contract). No block is triggered, so this can only succeed via the empty-block path.
func TestEmptyBlockBuilderReturnsEmptyBlockWhenShouldBuildEmpty(t *testing.T) {
	bb := testutil.NewTestControlledBlockBuilder(t)

	ebb := &simplex.EmptyBlockBuilder{
		// A short timeout makes ShouldBuildEmptyBlock fire promptly.
		Timeout:               10 * time.Millisecond,
		BB:                    bb,
		ShouldBuildEmptyBlock: blocksUntilDone,
	}

	block, ok := ebb.BuildBlock(context.Background(), common.ProtocolMetadata{}, common.NewBlacklist(1, uint64(1), simplex.DefaultMaxBlacklistedRounds))
	require.True(t, ok)
	require.NotNil(t, block)
}

// TestEmptyBlockBuilderReturnsNoBlockOnCallerCancel verifies that a caller-initiated
// cancellation aborts the underlying builder, which returns (nil, false).
func TestEmptyBlockBuilderReturnsNoBlockOnCallerCancel(t *testing.T) {
	bb := testutil.NewTestControlledBlockBuilder(t)

	ebb := &simplex.EmptyBlockBuilder{
		Timeout:               time.Hour,
		BB:                    bb,
		ShouldBuildEmptyBlock: blocksUntilDone,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // caller cancels before the call.

	block, ok := ebb.BuildBlock(ctx, common.ProtocolMetadata{}, common.NewBlacklist(1, uint64(1), simplex.DefaultMaxBlacklistedRounds))
	require.False(t, ok)
	require.Nil(t, block)
}

// TestEmptyBlockBuilderDoesNotCancelWhenShouldBuildReturnsFalse verifies that when the
// timeout elapses but ShouldBuildEmptyBlock declines (returns false, e.g. there is no
// notarized-but-unfinalized suffix), the underlying builder is NOT preempted and can
// still return a real block afterwards.
func TestEmptyBlockBuilderDoesNotCancelWhenShouldBuildReturnsFalse(t *testing.T) {
	bb := testutil.NewTestControlledBlockBuilder(t)
	declined := make(chan struct{})

	ebb := &simplex.EmptyBlockBuilder{
		Timeout: 10 * time.Millisecond,
		BB:      bb,
		ShouldBuildEmptyBlock: func(ctx context.Context) bool {
			<-ctx.Done() // wait for the empty-block timeout to elapse
			close(declined)
			return false // ...but decline to build an empty block
		},
	}

	// Only release the real block after ShouldBuildEmptyBlock has declined, so the
	// test deterministically exercises the "timeout fired, but no cancel" path.
	go func() {
		<-declined
		bb.TriggerNewBlock()
	}()

	block, ok := ebb.BuildBlock(context.Background(), common.ProtocolMetadata{}, common.NewBlacklist(1, uint64(1), simplex.DefaultMaxBlacklistedRounds))
	require.True(t, ok)
	require.NotNil(t, block)
}

// TestEmptyBlockBuilderCleansUpAfterBuild verifies that once BuildBlock returns, the
// spawned ShouldBuildEmptyBlock goroutine is cancelled (no goroutine leak).
func TestEmptyBlockBuilderCleansUpAfterBuild(t *testing.T) {
	bb := testutil.NewTestControlledBlockBuilder(t)
	exited := make(chan struct{})

	ebb := &simplex.EmptyBlockBuilder{
		Timeout: time.Hour,
		BB:      bb,
		ShouldBuildEmptyBlock: func(ctx context.Context) bool {
			<-ctx.Done()
			close(exited)
			return false
		},
	}

	bb.TriggerNewBlock()
	_, ok := ebb.BuildBlock(context.Background(), common.ProtocolMetadata{}, common.NewBlacklist(1, uint64(1), simplex.DefaultMaxBlacklistedRounds))
	require.True(t, ok)

	select {
	case <-exited:
	case <-time.After(time.Minute):
		require.Fail(t, "ShouldBuildEmptyBlock goroutine was not cancelled after BuildBlock returned")
	}
}

// TestEmptyBlockBuilderWaitForPendingBlockCancelledByShouldBuildEmpty verifies that
// when ShouldBuildEmptyBlock fires, WaitForPendingBlock cancels the underlying
// WaitForPendingBlock and returns.
func TestEmptyBlockBuilderWaitForPendingBlockCancelledByShouldBuildEmpty(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()

	ebb := &simplex.EmptyBlockBuilder{
		Timeout:               10 * time.Millisecond,
		BB:                    bb,
		ShouldBuildEmptyBlock: blocksUntilDone,
	}

	done := make(chan struct{})
	go func() {
		ebb.WaitForPendingBlock(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail(t, "WaitForPendingBlock did not return after ShouldBuildEmptyBlock fired")
	}
}

// TestEmptyBlockBuilderWaitForPendingBlockReturnsWhenUnderlyingReturns verifies that
// when the underlying builder signals a pending block, WaitForPendingBlock returns
// without waiting for the empty-block timeout.
func TestEmptyBlockBuilderWaitForPendingBlockReturnsWhenUnderlyingReturns(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()

	ebb := &simplex.EmptyBlockBuilder{
		// A long timeout ensures the empty-block path is not what unblocks us.
		Timeout:               time.Hour,
		BB:                    bb,
		ShouldBuildEmptyBlock: waitsForCancel,
	}

	done := make(chan struct{})
	go func() {
		ebb.WaitForPendingBlock(context.Background())
		close(done)
	}()

	bb.BlockShouldBeBuilt <- struct{}{} // application signals a pending block.

	select {
	case <-done:
	case <-time.After(time.Minute):
		require.Fail(t, "WaitForPendingBlock did not return after the underlying builder returned")
	}
}

// TestEmptyBlockBuilderWaitForPendingBlockCancelledByCaller verifies that a
// caller-initiated cancellation unblocks WaitForPendingBlock.
func TestEmptyBlockBuilderWaitForPendingBlockCancelledByCaller(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()

	ebb := &simplex.EmptyBlockBuilder{
		Timeout:               time.Hour,
		BB:                    bb,
		ShouldBuildEmptyBlock: waitsForCancel,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ebb.WaitForPendingBlock(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Minute):
		require.Fail(t, "WaitForPendingBlock did not return after the caller cancelled the context")
	}
}
