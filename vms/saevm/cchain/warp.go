// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

var _ warp.Backend = (*warpBackend)(nil)

type warpBackend struct {
	vm *sae.VM
}

func (w *warpBackend) IsAccepted(ctx context.Context, blkID ids.ID) error {
	b, err := w.vm.GetBlock(ctx, blkID)
	if err != nil {
		return fmt.Errorf("getting block: %w", err)
	}
	// Processing, non-canonical, blocks can be returned from GetBlock, so we
	// MUST verify that the block is canonical for its height.
	height := b.Height()
	acceptedID, err := w.vm.GetBlockIDAtHeight(ctx, height)
	if err != nil {
		return fmt.Errorf("getting block ID at height %d: %w", height, err)
	}
	if acceptedID != blkID {
		return fmt.Errorf("conflicting block %s was accepted at height %d", acceptedID, height)
	}
	return nil
}
