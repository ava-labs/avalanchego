// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

func noRelease() {}

// StateAt returns an isolated [state.StateDB] for b's post-execution state and a
// release function. The caller MUST call release after the state and its copies
// are no longer in use. Each state MUST be used by one goroutine.
//
// If Firewood cannot open b's revision, StateAt replays from the nearest earlier
// committed revision. reexec MAY extend, but MUST NOT reduce, the commit-interval
// replay horizon.
func (e *Executor) StateAt(ctx context.Context, b *blocks.Block, reexec uint64) (*state.StateDB, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	root := b.PostExecutionStateRoot()
	if !e.CanReconstruct() {
		stateDB, err := e.StateDB(root)
		if err != nil {
			return nil, nil, err
		}
		return stateDB, noRelease, nil
	}

	if stateDB, release, err := e.Reconstructing(root); err == nil {
		return stateDB, release, nil
	} else if !errors.Is(err, saedb.ErrStateUnavailable) {
		return nil, nil, err
	}

	select {
	case e.replaySlots <- struct{}{}:
		defer func() { <-e.replaySlots }()
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	return e.replayTo(ctx, b, max(reexec, e.commitInterval))
}

var (
	errNoSeedState        = errors.New("no state to seed reconstruction")
	errReplayRootMismatch = errors.New("replayed state root mismatch")
)

// replayTo reconstructs target with normal execution and replay-only hooks. It
// preserves deterministic state changes without canonical side effects.
func (e *Executor) replayTo(ctx context.Context, target *blocks.Block, replayHorizon uint64) (_ *state.StateDB, _ func(), retErr error) {
	stateDB, release, parent, err := e.seed(ctx, target, replayHorizon)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if retErr != nil {
			release()
		}
	}()
	replayHooks := replayPoints{Points: e.hooks}

	for h := parent.Height() + 1; h <= target.Height(); h++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		// Execute changes consensus-critical fields, so replay a new wrapper and
		// keep restored unchanged as the next block's parent.
		restored, err := e.restoreBlock(h)
		if err != nil {
			return nil, nil, err
		}
		replaying, err := blocks.New(restored.EthBlock(), parent, nil /* lastSettled */, e.log)
		if err != nil {
			return nil, nil, fmt.Errorf("constructing block %d for replay: %w", h, err)
		}

		result, err := Execute(replaying, stateDB, math.MaxInt, replayHooks, e.chainConfig, e.chainContext, &NullReceiptStore{}, e.log)
		if err != nil {
			return nil, nil, fmt.Errorf("replaying block %d: %w", h, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		// TODO(#5539): Flush each replayed block into the reconstructed trie without
		// calculating its root. This bounds StateDB memory and handles pre-Cancun
		// subnet histories that delete and recreate an account across replayed blocks.
		result.StateDB.Finalise(true /* EIP-158 */)
		parent = restored
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	got := stateDB.IntermediateRoot(true /* EIP-158 */)
	if want := target.PostExecutionStateRoot(); got != want {
		return nil, nil, fmt.Errorf("%w: replaying block %d produced %#x, want %#x", errReplayRootMismatch, target.Height(), got, want)
	}
	return stateDB, release, nil
}

// replayPoints redirects canonical after-block hooks to their replay-safe form.
type replayPoints struct {
	hook.Points
}

func (h replayPoints) AfterExecutingBlock(stateDB *state.StateDB, b *types.Block, receipts types.Receipts) error {
	return h.AfterReexecutingBlock(stateDB, b, receipts)
}

// seed returns a reconstructed [state.StateDB] at the nearest earlier committed
// state, along with the block that state belongs to. It searches at most
// replayHorizon blocks and skips unavailable revisions and uncommitted
// proposals.
func (e *Executor) seed(ctx context.Context, target *blocks.Block, replayHorizon uint64) (*state.StateDB, func(), *blocks.Block, error) {
	searchLimit := min(replayHorizon, target.Height())
	for distance := uint64(1); distance <= searchLimit; distance++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}

		// TODO(JonathanOppenheimer): Only the root is needed here. Restoring the
		// block is expensive. Read the root from the stored execution results instead,
		// and restore only the block this returns.
		h := target.Height() - distance
		bl, err := e.restoreBlock(h)
		if err != nil {
			return nil, nil, nil, err
		}
		if stateDB, release, err := e.Reconstructing(bl.PostExecutionStateRoot()); err == nil {
			return stateDB, release, bl, nil
		} else if !errors.Is(err, saedb.ErrStateUnavailable) {
			return nil, nil, nil, err
		}
	}
	return nil, nil, nil, fmt.Errorf("%w: searched %d blocks below block %d", errNoSeedState, searchLimit, target.Height())
}

// restoreBlock restores canonical block h with its recorded execution results.
func (e *Executor) restoreBlock(h uint64) (*blocks.Block, error) {
	ethB := rawdb.ReadBlock(e.db, rawdb.ReadCanonicalHash(e.db, h), h)
	if ethB == nil {
		return nil, fmt.Errorf("no canonical block at height %d", h)
	}

	bl, err := blocks.RestoreSettledBlock(ethB, e.hooks, e.log, e.db, e.xdb, e.chainConfig)
	if err != nil {
		return nil, fmt.Errorf("restoring block %d: %w", h, err)
	}
	return bl, nil
}
