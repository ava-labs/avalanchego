// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"time"

	"github.com/ava-labs/simplex/common"
)

// EmptyBlockBuilder is a BlockBuilder that builds an empty block if the given shouldBuildEmptyBlock function returns true.
// The given shouldBuildEmptyBlock function blocks until the given context is cancelled.
type EmptyBlockBuilder struct {
	Timeout               time.Duration
	BB                    common.BlockBuilder
	ShouldBuildEmptyBlock func(context.Context) bool
}

// BuildBlock builds a block using the underlying BlockBuilder.
// If within the timeout, shouldBuildEmptyBlock returns true, it cancels the context with ErrShouldBuildEmptyBlock.
func (ebb *EmptyBlockBuilder) BuildBlock(ctx context.Context, metadata common.ProtocolMetadata, blacklist common.Blacklist) (common.VerifiedBlock, bool) {
	ctx, outerCancel := context.WithCancelCause(ctx)
	defer outerCancel(nil)

	go func() {
		innerContext, innerCancel := context.WithTimeoutCause(ctx, ebb.Timeout, common.ErrShouldBuildEmptyBlock)
		defer innerCancel()

		if ebb.ShouldBuildEmptyBlock(innerContext) {
			outerCancel(common.ErrShouldBuildEmptyBlock)
		}
	}()
	return ebb.BB.BuildBlock(ctx, metadata, blacklist)
}

// WaitForPendingBlock waits for the underlying BlockBuilder to have a pending block.
// If within the timeout, shouldBuildEmptyBlock returns true, it cancels the context with ErrShouldBuildEmptyBlock.
func (ebb *EmptyBlockBuilder) WaitForPendingBlock(ctx context.Context) {
	ctx, outerCancel := context.WithCancel(ctx)
	defer outerCancel()

	go func() {
		innerContext, innerCancel := context.WithTimeoutCause(ctx, ebb.Timeout, common.ErrShouldBuildEmptyBlock)
		defer innerCancel()

		if ebb.ShouldBuildEmptyBlock(innerContext) {
			outerCancel()
		}
	}()

	ebb.BB.WaitForPendingBlock(ctx)
}
