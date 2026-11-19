// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"context"
	"time"

	"github.com/ava-labs/simplex/common"
)

type TestBlockBuilder struct {
	// built is a channel that holds built test blocks
	built              chan *TestBlock
	BlockShouldBeBuilt chan struct{}
}

func NewTestBlockBuilder() *TestBlockBuilder {
	return &TestBlockBuilder{
		built:              make(chan *TestBlock, 1),
		BlockShouldBeBuilt: make(chan struct{}, 1),
	}
}

func (t *TestBlockBuilder) WithBuiltBuffer(buffer uint64) *TestBlockBuilder {
	t.built = make(chan *TestBlock, buffer)
	return t
}

func (t *TestBlockBuilder) WithBlockShouldBeBuiltBuffer(buffer uint64) *TestBlockBuilder {
	t.BlockShouldBeBuilt = make(chan struct{}, buffer)
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

func (t *TestBlockBuilder) WaitForPendingBlock(ctx context.Context) {
	select {
	case <-t.BlockShouldBeBuilt:
	case <-ctx.Done():
	}
}
