// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
)

var (
	ErrNoReconstructionSeed      = errors.New("no state to seed reconstruction")
	ErrReconstructedRootMismatch = errors.New("reconstructed state root mismatch")
)

func noRelease() {}

// reconstructState returns target's post-execution state. It keeps the normal
// state-opening path unchanged and reconstructs only unavailable Firewood
// state.
func (b *backend) reconstructState(ctx context.Context, target *blocks.Block) (*state.StateDB, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	root := target.PostExecutionStateRoot()
	stateDB, err := b.StateDB(root)
	if err == nil {
		return stateDB, noRelease, nil
	}
	if !errors.Is(err, saedb.ErrStateUnavailable) || !b.CanReconstruct() {
		return nil, nil, err
	}

	stateDB, release, err := b.Reconstructing(root)
	if err == nil {
		return stateDB, release, nil
	}
	if !errors.Is(err, saedb.ErrStateUnavailable) {
		return nil, nil, err
	}

	select {
	case b.replaySlots <- struct{}{}:
		defer func() { <-b.replaySlots }()
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	stateDB, release, seed, err := b.reconstructionSeed(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	failed := true
	defer func() {
		if failed {
			release()
		}
	}()

	parent := seed
	for height := seed.Height() + 1; height <= target.Height(); height++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		restored, err := b.restoreExecutedBlockAtHeight(ctx, height)
		if err != nil {
			return nil, nil, err
		}
		// Restored settled blocks do not retain their parents. Rewrap the block
		// with its replay parent so execution has the required ancestry.
		replaying, err := b.NewBlock(restored.EthBlock(), parent, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("constructing block %d for replay: %v", height, err)
		}
		if _, err := saexec.Execute(
			replaying,
			stateDB,
			b.Hooks(),
			b.ChainConfig(),
			b.ChainContext(),
			b.Logger(),
		); err != nil {
			return nil, nil, fmt.Errorf("replaying block %d: %w", height, err)
		}
		// Finalise makes end-of-block state changes visible to the next block
		// without calculating an intermediate root.
		stateDB.Finalise(true /* deleteEmptyObjects */)
		parent = restored
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	got := stateDB.IntermediateRoot(true /* deleteEmptyObjects */)
	if got != root {
		return nil, nil, fmt.Errorf("%w: block %d produced %#x, want %#x", ErrReconstructedRootMismatch, target.Height(), got, root)
	}

	failed = false
	return stateDB, release, nil
}

func (b *backend) reconstructionSeed(ctx context.Context, target *blocks.Block) (*state.StateDB, func(), *blocks.Block, error) {
	// Settlement may lag execution, so persisted revisions are not bounded by
	// their block distance from the target.
	for height := target.Height(); height > 0; {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		height--
		block, err := b.restoreExecutedBlockAtHeight(ctx, height)
		if err != nil {
			return nil, nil, nil, err
		}
		stateDB, release, err := b.Reconstructing(block.PostExecutionStateRoot())
		if err == nil {
			return stateDB, release, block, nil
		}
		if !errors.Is(err, saedb.ErrStateUnavailable) {
			return nil, nil, nil, err
		}
	}
	return nil, nil, nil, fmt.Errorf("%w: searched %d blocks below block %d", ErrNoReconstructionSeed, target.Height(), target.Height())
}

func (b *backend) restoreExecutedBlockAtHeight(ctx context.Context, height uint64) (*blocks.Block, error) {
	num := rpc.BlockNumber(height) // #nosec G115 -- block heights fit for the foreseeable future.
	block, err := b.restoreExecutedBlock(ctx, rpc.BlockNumberOrHashWithNumber(num))
	if err != nil {
		return nil, fmt.Errorf("restoring block %d: %w", height, err)
	}
	return block, nil
}
