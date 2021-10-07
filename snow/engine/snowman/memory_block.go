// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

// BlockWrapper wraps a snowman Block while adding a smart caching layer to improve
// VM performance.
type MemoryBlock struct {
	snowman.Block

	tree AncestorTree
}

// Accept accepts the underlying block, removes sibling subtrees.
func (mb *MemoryBlock) Accept() error {
	mb.tree.Purge(mb.Parent())
	return mb.Block.Accept()
}

// Reject rejects the underlying block, removes child subtress
func (mb *MemoryBlock) Reject() error {
	mb.tree.Purge(mb.ID())
	return mb.Block.Reject()
}
