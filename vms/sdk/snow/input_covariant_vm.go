// Copyright (C) 2024, Ava Labs, Inv. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

// InputCovariantVM provides basic VM functionality that only returns the input block type
// Input is the only field guaranteed to be set for each consensus block, so we provide
// a wrapper that only exposes the input block type.
type InputCovariantVM[I Block, O Block, A Block] struct {
	vm *VM[I, O, A]
}

func (v *InputCovariantVM[I, O, A]) GetBlock(ctx context.Context, blkID ids.ID) (I, error) {
	blk, err := v.vm.GetBlock(ctx, blkID)
	if err != nil {
		var emptyI I
		return emptyI, err
	}
	return blk.Input, nil
}

func (v *InputCovariantVM[I, O, A]) GetBlockByHeight(ctx context.Context, height uint64) (I, error) {
	blk, err := v.vm.GetBlockByHeight(ctx, height)
	if err != nil {
		var emptyI I
		return emptyI, err
	}
	return blk.Input, nil
}

func (v *InputCovariantVM[I, O, A]) ParseBlock(ctx context.Context, bytes []byte) (I, error) {
	blk, err := v.vm.ParseBlock(ctx, bytes)
	if err != nil {
		var emptyI I
		return emptyI, err
	}
	return blk.Input, nil
}

func (v *InputCovariantVM[I, O, A]) LastAcceptedBlock(ctx context.Context) I {
	return v.vm.LastAcceptedBlock(ctx).Input
}
