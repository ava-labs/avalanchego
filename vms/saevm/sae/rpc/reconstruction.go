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
)

var (
	ErrNoReconstructionSeed      = errors.New("no state to seed reconstruction")
	ErrReconstructedRootMismatch = errors.New("reconstructed state root mismatch")
)

func noRelease() {}

// reconstructState returns target's post-execution state. It keeps the normal
// state-opening path unchanged and reconstructs only unavailable Firewood
// state.
func (b *backend) reconstructState(ctx context.Context, target *blocks.Block, reexec uint64) (*state.StateDB, func(), error) {
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

	horizon := max(reexec, b.CommitInterval())
	stateDB, release, seed, err := b.reconstructionSeed(ctx, target, horizon)
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
		// Execution mutates consensus-derived fields on the wrapper. Always use
		// a fresh wrapper so the restored canonical object remains unchanged.
		replaying, err := b.NewBlock(restored.EthBlock(), parent, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("constructing block %d for replay: %v", height, err)
		}
		if err := b.BlockProcessor().ExecuteBlock(replaying, stateDB); err != nil {
			return nil, nil, fmt.Errorf("replaying block %d: %w", height, err)
		}

		// IntermediateRoot flushes each complete block into the mutable
		// reconstructed view. This is required when one block deletes an
		// account and a later block recreates it.
		got := stateDB.IntermediateRoot(true /* deleteEmptyObjects */)
		want := restored.PostExecutionStateRoot()
		if got != want {
			return nil, nil, fmt.Errorf("%w: block %d produced %#x, want %#x", ErrReconstructedRootMismatch, height, got, want)
		}
		parent = restored
	}

	failed = false
	return stateDB, release, nil
}

func (b *backend) reconstructionSeed(ctx context.Context, target *blocks.Block, horizon uint64) (*state.StateDB, func(), *blocks.Block, error) {
	searchLimit := min(horizon, target.Height())
	for distance := uint64(1); distance <= searchLimit; distance++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		height := target.Height() - distance
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
	return nil, nil, nil, fmt.Errorf("%w: searched %d blocks below block %d", ErrNoReconstructionSeed, searchLimit, target.Height())
}

func (b *backend) restoreExecutedBlockAtHeight(ctx context.Context, height uint64) (*blocks.Block, error) {
	num := rpc.BlockNumber(height) // #nosec G115 -- block heights fit for the foreseeable future.
	block, err := b.restoreExecutedBlock(ctx, rpc.BlockNumberOrHashWithNumber(num))
	if err != nil {
		return nil, fmt.Errorf("restoring block %d: %w", height, err)
	}
	return block, nil
}
