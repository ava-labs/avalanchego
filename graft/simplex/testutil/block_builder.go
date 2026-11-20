// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/graft/simplex/simplex"
)

type TestBlockBuilder struct {
	// built is a channel that holds built test blocks
	built chan *TestBlock

	// CurrentRound returns the current round of the epoch that uses this block builder.
	// It must be set before calling TriggerBlockShouldBeBuilt.
	CurrentRound func() uint64

	lock sync.Mutex
	// pendingChanged is signaled whenever a round is added to pendingRounds.
	pendingChanged *sync.Cond
	// pendingRounds are the rounds in which a block should be built.
	pendingRounds map[uint64]struct{}
}

func NewTestBlockBuilder() *TestBlockBuilder {
	t := &TestBlockBuilder{
		built:         make(chan *TestBlock, 1),
		pendingRounds: make(map[uint64]struct{}),
	}
	t.pendingChanged = sync.NewCond(&t.lock)
	return t
}

// RoundOf returns a function that returns the current round of the given epoch,
// to be used as [TestBlockBuilder.CurrentRound].
func RoundOf(e *simplex.Epoch) func() uint64 {
	return func() uint64 {
		return e.Metadata().Round
	}
}

func (t *TestBlockBuilder) WithBuiltBuffer(buffer uint64) *TestBlockBuilder {
	t.built = make(chan *TestBlock, buffer)
	return t
}

func (t *TestBlockBuilder) BuildBlock(_ context.Context, metadata common.ProtocolMetadata, blacklist common.Blacklist) (common.VerifiedBlock, bool) {
	tb := NewTestBlock(metadata, blacklist)

	select {
	case t.built <- tb:
		return tb, true
	default:
	}
	return tb, true
}

func (t *TestBlockBuilder) GetBuiltBlock() *TestBlock {
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	select {
	case b := <-t.built:
		return b
	case <-timeout.C:
		panic("timed out waiting for built block")
	}
}

func (t *TestBlockBuilder) SetBuiltBlock(block *TestBlock) {
	select {
	case t.built <- block:
	default:
		panic("built channel is full")
	}
}

// TriggerBlockShouldBeBuilt signals that a block should be built in the given round.
// Only a WaitForPendingBlock invocation of that round returns due to it.
func (t *TestBlockBuilder) TriggerBlockShouldBeBuilt(round uint64) {
	if t.CurrentRound == nil {
		panic("CurrentRound must be set before triggering a block to be built")
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	t.pendingRounds[round] = struct{}{}
	t.pendingChanged.Broadcast()
}

// WaitForPendingBlock returns once a block should be built in the current round,
// or once ctx is cancelled.
func (t *TestBlockBuilder) WaitForPendingBlock(ctx context.Context) {
	if t.CurrentRound == nil {
		<-ctx.Done()
		return
	}

	// Wake up the waiter below once ctx is cancelled.
	// Broadcasting under the lock ensures the wakeup is not lost between checking ctx and waiting.
	stop := context.AfterFunc(ctx, func() {
		t.lock.Lock()
		defer t.lock.Unlock()
		t.pendingChanged.Broadcast()
	})
	defer stop()

	for {
		// The round is read before checking ctx, as the context is cancelled before the round advances.
		// Therefore, if ctx is not cancelled, the round we read is the round we are waiting for.
		// It is read without holding the lock, as it acquires the epoch's lock.
		round := t.CurrentRound()

		t.lock.Lock()
		if ctx.Err() != nil {
			t.lock.Unlock()
			return
		}
		if _, pending := t.pendingRounds[round]; pending {
			t.lock.Unlock()
			return
		}
		t.pendingChanged.Wait()
		t.lock.Unlock()
	}
}
