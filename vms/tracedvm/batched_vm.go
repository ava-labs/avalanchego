// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	if vm.bVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, "blockVM.GetAncestors", oteltrace.WithAttributes(
		attribute.Stringer("blkID", blkID),
		attribute.Int("maxBlocksNum", maxBlocksNum),
		attribute.Int("maxBlocksSize", maxBlocksSize),
		attribute.Int64("maxBlocksRetrivalTime", int64(maxBlocksRetrivalTime)),
	))
	defer span.End()

	return vm.bVM.GetAncestors(
		ctx,
		blkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
}

func (vm *blockVM) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]snowman.Block, error) {
	if vm.bVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	ctx, span := vm.tracer.Start(ctx, "blockVM.BatchedParseBlock", oteltrace.WithAttributes(
		attribute.Int("numBlocks", len(blks)),
	))
	defer span.End()

	blocks, err := vm.bVM.BatchedParseBlock(ctx, blks)
	if err != nil {
		return nil, err
	}

	wrappedBlocks := make([]snowman.Block, len(blocks))
	for i, block := range blocks {
		wrappedBlocks[i] = &tracedBlock{
			Block: block,
			vm:    vm,
		}
	}
	return wrappedBlocks, nil
}
