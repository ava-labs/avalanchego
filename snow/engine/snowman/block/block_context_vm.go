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
	// PChainHeight is the height that this block will use to verify it's state.
	// In the proposervm, blocks verify the proposer based on the P-chain height
	// recorded in the parent block. The P-chain height provided here is also
	// the parent's P-chain height, not this block's P-chain height.
	//
	// Because PreForkBlocks and PostForkOptions do not verify their execution
	// against the P-chain's state, this context is undefined for those blocks.
	PChainHeight uint64
}

// BuildBlockWithContextChainVM defines the interface a ChainVM can optionally
// implement to consider the P-Chain height when building blocks.
type BuildBlockWithContextChainVM interface {
	// Attempt to build a new block given that the P-Chain height is
	// [blockCtx.PChainHeight].
	//
	// This method will be called if and only if the proposervm is activated.
	// Otherwise [BuildBlock] will be called.
	BuildBlockWithContext(ctx context.Context, blockCtx *Context) (snowman.Block, error)
}

type WithVerifyContext interface {
	// Returns true if [VerifyWithContext] should be called.
	// Returns false if [Verify] should be called.
	//
	// This method will be called if and only if the proposervm is activated.
	// Otherwise [Verify] will be called.
	ShouldVerifyWithContext(context.Context) (bool, error)

	// Verify that the state transition this block would make if accepted is
	// valid. If the state transition is invalid, a non-nil error should be
	// returned.
	//
	// It is guaranteed that the Parent has been successfully verified.
	//
	// This method may be called again with a different context.
	VerifyWithContext(context.Context, *Context) error
}
