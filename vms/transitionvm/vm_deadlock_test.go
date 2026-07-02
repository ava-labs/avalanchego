// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// TestTransitionCtxLockDeadlock demonstrates the transition holds ctx.Lock and wants
// transitionLock, a gossip holds transitionLock and wants ctx.Lock.
func TestTransitionCtxLockDeadlock(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()
	appGossipTakesCtxLock(sut)

	blk, err := sut.BuildBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, blk.Verify(ctx))

	// The handler holds ctx.Lock across Accept.
	sut.pre.chainCtx.Lock.Lock()
	defer sut.pre.chainCtx.Lock.Unlock()

	// The gossip grabs transitionLock, then waits on ctx.Lock.
	go func() { _ = sut.AppGossip(ctx, ids.GenerateTestNodeID(), nil) }()
	time.Sleep(100 * time.Millisecond)

	// Accept wants transitionLock, held by the gossip.
	accepted := make(chan struct{})
	go func() {
		_ = blk.Accept(ctx)
		close(accepted)
	}()

	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("transition deadlocked")
	}
}

// TestTransitionCtxLockControl demonstrates with no one holding ctx.Lock, the same gossip
// and transition finish. Shows the deadlock is the lock order, not the setup.
func TestTransitionCtxLockControl(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()
	appGossipTakesCtxLock(sut)

	blk, err := sut.BuildBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, blk.Verify(ctx))

	go func() { _ = sut.AppGossip(ctx, ids.GenerateTestNodeID(), nil) }()
	require.NoError(t, blk.Accept(ctx))
}

func appGossipTakesCtxLock(sut *SUT) {
	sut.pre.VM.AppGossipF = func(context.Context, ids.NodeID, []byte) error {
		sut.pre.chainCtx.Lock.RLock()
		sut.pre.chainCtx.Lock.RUnlock()
		return nil
	}
}
