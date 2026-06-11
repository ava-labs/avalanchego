// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package cchain implements the C-Chain VM atop [sae.VM]. It composes the
// C-Chain block-building hooks, the cross-chain transaction pool, and the avax
// JSON-RPC service that ingests Export and Import transactions.
package cchain

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

// warpBackend adapts [sae.VM] to the [warp.Backend] interface.
type warpBackend struct {
	vm *sae.VM
}

var _ warp.Backend = (*warpBackend)(nil)

func (w *warpBackend) IsAccepted(ctx context.Context, blkID ids.ID) error {
	b, err := w.vm.GetBlock(ctx, blkID)
	if err != nil {
		return fmt.Errorf("getting block: %w", err)
	}
	// Processing, and not yet accepted, blocks can be returned from GetBlock,
	// so we need to additionally verify that the block is canonical for its
	// height.
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
