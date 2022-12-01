// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

// Context defines the block context that will be optionally provided by the
// proposervm to an underlying vm.
type Context struct {
	PChainHeight uint64
}

// BuildBlockWithContextChainVM defines the interface a ChainVM can optionally
// implement to consider the P-Chain height when building blocks.
type BuildBlockWithContextChainVM interface {
	// Attempt to build a new block given that the P-Chain height is
	// [blockCtx.PChainHeight].
	// This method will be called if and only if the proposervm is activated.
	// Otherwise [BuildBlock] will be called.
	BuildBlockWithContext(ctx context.Context, blockCtx *Context) (snowman.Block, error)
}
