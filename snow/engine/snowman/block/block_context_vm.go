// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

// Context defines the block context that will be optionally provided by the
// proposervm to an underlying vm.
type Context struct {
	// PChainHeight is the height that this block will use to verify it's state.
	// In the proposervm, blocks verify the proposer based on the P-chain height
	// recorded in the parent block. However, the P-chain height provided here
	// is the P-chain height encoded into this block.
	//
	// Pre-Etna this value matched the parent block's P-chain height.
	//
	// Because PreForkBlocks and PostForkOptions do not verify their execution
	// against the P-chain's state, this context is undefined for those blocks.
	PChainHeight uint64 `canoto:"uint,1" json:"pChainHeight"`

	canotoData canotoData_Context
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

// WithVerifyContext defines the interface a Block can optionally implement to
// consider the P-Chain height when verifying itself.
//
// As with all Blocks, it is guaranteed for verification to be called in
// topological order.
//
// If the status of the block is Accepted or Rejected; VerifyWithContext will
// never be called.
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
	//
	// If nil is returned, it is guaranteed that either Accept or Reject will be
	// called on this block, unless the VM is shut down.
	//
	// Note: During `Accept` the block context is not provided. This implies
	// that the block context provided here can not be used to alter any
	// potential state transition that assumes network agreement. The block
	// context should only be used to determine the validity of the block.
	VerifyWithContext(context.Context, *Context) error
}
