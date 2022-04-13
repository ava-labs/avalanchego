// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/chain4travel/caminogo/snow/consensus/snowman"
)

// memoryBlock wraps a snowman Block to manage non-verified blocks
type memoryBlock struct {
	snowman.Block

	tree    AncestorTree
	metrics *metrics
}

// Accept accepts the underlying block & removes sibling subtrees
func (mb *memoryBlock) Accept() error {
	mb.tree.RemoveSubtree(mb.Parent())
	mb.metrics.numNonVerifieds.Set(float64(mb.tree.Len()))
	return mb.Block.Accept()
}

// Reject rejects the underlying block & removes child subtrees
func (mb *memoryBlock) Reject() error {
	mb.tree.RemoveSubtree(mb.ID())
	mb.metrics.numNonVerifieds.Set(float64(mb.tree.Len()))
	return mb.Block.Reject()
}
