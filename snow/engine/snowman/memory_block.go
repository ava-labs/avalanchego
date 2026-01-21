// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/ancestor"
)

var _ snowman.Block = (*memoryBlock)(nil)

// memoryBlock wraps a snowman Block to manage non-verified blocks
type memoryBlock struct {
	snowman.Block

	tree    ancestor.Tree
	metrics *metrics
}

// Accept accepts the underlying block & removes sibling subtrees
func (mb *memoryBlock) Accept(ctx context.Context) error {
	mb.tree.RemoveDescendants(mb.Parent())
	mb.metrics.numNonVerifieds.Set(float64(mb.tree.Len()))
	return mb.Block.Accept(ctx)
}

// Reject rejects the underlying block & removes child subtrees
func (mb *memoryBlock) Reject(ctx context.Context) error {
	mb.tree.RemoveDescendants(mb.ID())
	mb.metrics.numNonVerifieds.Set(float64(mb.tree.Len()))
	return mb.Block.Reject(ctx)
}
