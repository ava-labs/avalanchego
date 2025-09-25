// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	if vm.buildBlockVM == nil {
		return vm.BuildBlock(ctx)
	}

	start := time.Now()
	blk, err := vm.buildBlockVM.BuildBlockWithContext(ctx, blockCtx)
	end := time.Now()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.blockMetrics.buildBlockWithContextErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.buildBlockWithContext.Observe(duration)
	return &meterBlock{
		Block: blk,
		vm:    vm,
	}, nil
}
