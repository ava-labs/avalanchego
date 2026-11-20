// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/graft/simplex/simplex"
	"github.com/stretchr/testify/require"
)

type TestBlockBuilder struct {
	// built is a channel that holds built test blocks
	built chan *TestBlock

	// currentRound returns the current round of the epoch that uses this block builder.
	currentRound func() uint64

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
		currentRound: func() uint64 {
			return 0
		},
	}
	t.pendingChanged = sync.NewCond(&t.lock)
	return t
}

// NewEpoch creates an epoch from conf and binds bb to the epoch's current round.
// bb must be the block builder of conf.
func NewEpoch(t *testing.T, conf simplex.EpochConfig, bb *TestBlockBuilder) *simplex.Epoch {
	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	bb.currentRound = func() uint64 {
		return e.Metadata().Round
	}
	return e
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

// TriggerBlockShouldBeBuilt signals that a block should be built in the current round.
func (t *TestBlockBuilder) TriggerBlockShouldBeBuilt() {
	if t.currentRound == nil {
		panic("CurrentRound must be set before triggering a block to be built")
	}

	t.TriggerBlockShouldBeBuiltInRound(t.currentRound())
}

// TriggerBlockShouldBeBuiltInRound signals that a block should be built in the given round.
func (t *TestBlockBuilder) TriggerBlockShouldBeBuiltInRound(round uint64) {
	if t.currentRound == nil {
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
	stop := context.AfterFunc(ctx, func() {
		t.lock.Lock()
		defer t.lock.Unlock()
		t.pendingChanged.Broadcast()
	})
	defer stop()

	for {
		round := t.currentRound()

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
