// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const pruneCommitPeriod = 1024

// vm.ctx.Lock should be held
func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	switch forkHeight, err := vm.State.GetForkHeight(); err {
	case nil:
		if height < forkHeight {
			return vm.ChainVM.GetBlockIDAtHeight(ctx, height)
		}
		return vm.State.GetBlockIDAtHeight(height)

	case database.ErrNotFound:
		// fork not reached yet. Block must be pre-fork
		return vm.ChainVM.GetBlockIDAtHeight(ctx, height)

	default:
		return ids.Empty, err
	}
}

func (vm *VM) updateHeightIndex(height uint64, blkID ids.ID) error {
	forkHeight, err := vm.State.GetForkHeight()
	switch err {
	case nil:
		// The fork was already reached. Just update the index.

	case database.ErrNotFound:
		// This is the first post fork block, store the fork height.
		if err := vm.State.SetForkHeight(height); err != nil {
			return fmt.Errorf("failed storing fork height: %w", err)
		}
		forkHeight = height

	default:
		return fmt.Errorf("failed to load fork height: %w", err)
	}

	if err := vm.State.SetBlockIDAtHeight(height, blkID); err != nil {
		return err
	}

	vm.ctx.Log.Debug("indexed block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", height),
	)

	if vm.NumHistoricalBlocks == 0 {
		return nil
	}

	blocksSinceFork := height - forkHeight
	// Note: The last accepted block is not considered a historical block. Which
	// is why <= is used rather than <. This prevents the user from only storing
	// the last accepted block, which can never be safe due to the non-atomic
	// commits between the proposervm database and the innerVM's database.
	if blocksSinceFork <= vm.NumHistoricalBlocks {
		return nil
	}

	// Note: heightToDelete is >= forkHeight, so it is guaranteed not to
	// underflow.
	heightToDelete := height - vm.NumHistoricalBlocks - 1
	blockToDelete, err := vm.State.GetBlockIDAtHeight(heightToDelete)
	if err == database.ErrNotFound {
		// Block may have already been deleted. This can happen due to a
		// proposervm rollback, the node having recently state-synced, or the
		// user reconfiguring the node to store more historical blocks than a
		// prior run.
		return nil
	}
	if err != nil {
		return err
	}

	if err := vm.State.DeleteBlockIDAtHeight(heightToDelete); err != nil {
		return err
	}
	if err := vm.State.DeleteBlock(blockToDelete); err != nil {
		return err
	}

	vm.ctx.Log.Debug("deleted block",
		zap.Stringer("blkID", blockToDelete),
		zap.Uint64("height", heightToDelete),
	)
	return nil
}

// TODO: Support async deletion of old blocks.
func (vm *VM) pruneOldBlocks() error {
	if vm.NumHistoricalBlocks == 0 {
		return nil
	}

	height, err := vm.State.GetMinimumHeight()
	if err == database.ErrNotFound {
		// Chain hasn't forked yet
		return nil
	}

	// TODO: Refactor to use DB iterators.
	//
	// Note: vm.lastAcceptedHeight is guaranteed to be >= height, so the
	// subtraction can never underflow.
	for vm.lastAcceptedHeight-height > vm.NumHistoricalBlocks {
		blockToDelete, err := vm.State.GetBlockIDAtHeight(height)
		if err != nil {
			return err
		}

		if err := vm.State.DeleteBlockIDAtHeight(height); err != nil {
			return err
		}
		if err := vm.State.DeleteBlock(blockToDelete); err != nil {
			return err
		}

		vm.ctx.Log.Debug("deleted block",
			zap.Stringer("blkID", blockToDelete),
			zap.Uint64("height", height),
		)

		// Note: height is < vm.lastAcceptedHeight, so it is guaranteed not to
		// overflow.
		height++
		if height%pruneCommitPeriod != 0 {
			continue
		}

		if err := vm.db.Commit(); err != nil {
			return err
		}
	}
	return vm.db.Commit()
}
