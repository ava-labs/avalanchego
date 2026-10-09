// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

var (
	errForkBlockUnsigned = errors.New("block at or after the fork time must be signed")

	forkPointKey = []byte("fork point")
)

// initFork prepares fork mode, if configured: it builds the fork windower and
// re-reports a previously recorded fork point.
func (vm *VM) initFork() error {
	if vm.Fork == nil {
		return nil
	}
	vm.forkWindower = proposer.New(fork.NewStaticState(vm.Fork.Config()), vm.ctx.SubnetID, vm.ctx.ChainID, vm.ctx.Log)

	b, err := vm.db.Get(forkPointKey)
	switch {
	case errors.Is(err, database.ErrNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("reading fork point: %w", err)
	}
	point, err := fork.ParseForkPoint(b)
	if err != nil {
		return err
	}
	vm.forkPointKnown = true
	vm.Fork.SetForkPoint(vm.ctx.ChainID, point)
	return nil
}

// isForkBlock reports whether a block timestamped [ts] is subject to the fork
// proposer rule.
func (vm *VM) isForkBlock(ts time.Time) bool {
	return vm.Fork != nil && vm.Fork.Config().IsForked(ts)
}

// windowerFor returns the windower that schedules proposers for a block
// timestamped [ts].
func (vm *VM) windowerFor(ts time.Time) proposer.Windower {
	if vm.isForkBlock(ts) {
		return vm.forkWindower
	}
	return vm.Windower
}

// verifyForkProposer checks that [blk], timestamped at or after the fork
// time, is signed by the fork validator scheduled for its slot.
func (p *postForkCommonComponents) verifyForkProposer(
	ctx context.Context,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	blk *postForkBlock,
) error {
	proposerID := blk.Proposer()
	if proposerID == ids.EmptyNodeID {
		return errForkBlockUnsigned
	}

	slot := proposer.TimeToSlot(parentTimestamp, blk.Timestamp())
	blk.slot = &slot
	expectedProposerID, err := p.vm.forkWindower.ExpectedProposer(ctx, blk.Height(), parentPChainHeight, slot)
	if err != nil {
		return fmt.Errorf("calculating expected fork proposer: %w", err)
	}
	if expectedProposerID != proposerID {
		return fmt.Errorf("%w: slot %d expects %s", errUnexpectedProposer, slot, expectedProposerID)
	}
	return nil
}

// recordForkPoint persists this chain's fork point when [blk] is the first
// accepted block timestamped at or after the fork time. It must be called
// before the accept commit so that the fork point is written atomically with
// [blk].
func (vm *VM) recordForkPoint(blk PostForkBlock) error {
	if vm.Fork == nil || vm.forkPointKnown || !vm.Fork.Config().IsForked(blk.Timestamp()) {
		return nil
	}
	point := fork.ForkPoint{
		BlockID: blk.Parent(),
		Height:  blk.Height() - 1,
	}
	b, err := point.Bytes()
	if err != nil {
		return fmt.Errorf("encoding fork point: %w", err)
	}
	if err := vm.db.Put(forkPointKey, b); err != nil {
		return fmt.Errorf("writing fork point: %w", err)
	}
	vm.forkPointKnown = true
	vm.ctx.Log.Info("recorded fork point",
		zap.Stringer("blkID", point.BlockID),
		zap.Uint64("height", point.Height),
		zap.Stringer("firstForkBlkID", blk.ID()),
		zap.Time("firstForkBlkTimestamp", blk.Timestamp()),
	)
	vm.Fork.SetForkPoint(vm.ctx.ChainID, point)
	return nil
}
