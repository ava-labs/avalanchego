// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"go.uber.org/zap"

	"github.com/ava-labs/strevm/blocks"
	saetypes "github.com/ava-labs/strevm/types"
)

// maxFutureBlockDuration is the maximum time from the current time allowed for
// blocks before they're considered future blocks and fail parsing or
// verification.
const (
	maxFutureBlockSeconds  uint64 = 10
	maxFutureBlockDuration        = time.Duration(maxFutureBlockSeconds) * time.Second
)

var (
	errBlockHeightNotUint64 = errors.New("block height not uint64")
	errBlockTooFarInFuture  = errors.New("block too far in the future")
)

// ParseBlock parses the buffer as [rlp] encoding of a [types.Block]. It does
// NOT populate the block ancestry, which is done by [VM.VerifyBlock] i.f.f.
// verification passes.
func (vm *VM) ParseBlock(ctx context.Context, buf []byte) (*blocks.Block, error) {
	b := new(types.Block)
	if err := rlp.DecodeBytes(buf, b); err != nil {
		return nil, fmt.Errorf("rlp.DecodeBytes(..., %T): %v", b, err)
	}

	if !b.Number().IsUint64() {
		return nil, errBlockHeightNotUint64
	}
	// The uint64 timestamp can't underflow [time.Time] but it can overflow so
	// make this some future engineer's problem in a few millennia.
	if b.Time() > unix(vm.config.Now())+maxFutureBlockSeconds {
		return nil, fmt.Errorf("%w: >%s", errBlockTooFarInFuture, maxFutureBlockDuration)
	}

	return vm.blockBuilder.new(b, nil, nil)
}

// BuildBlock builds a new block, using the last block passed to
// [VM.SetPreference] as the parent. The block context MAY be nil.
func (vm *VM) BuildBlock(ctx context.Context, bCtx *block.Context) (*blocks.Block, error) {
	return vm.blockBuilder.build(ctx, bCtx, vm.preference.Load())
}

var (
	errUnknownParent     = errors.New("unknown parent")
	errBlockHeightTooLow = errors.New("block height too low")
	errHashMismatch      = errors.New("hash mismatch")
)

// VerifyBlock validates the block and, if successful, populates its ancestry.
// The block context MAY be nil.
func (vm *VM) VerifyBlock(ctx context.Context, bCtx *block.Context, b *blocks.Block) error {
	parent, err := vm.GetBlock(ctx, b.Parent())
	if err != nil {
		return fmt.Errorf("%w %#x: %w", errUnknownParent, b.ParentHash(), err)
	}

	// Sanity check that we aren't verifying an accepted block.
	if height, accepted := b.Height(), vm.last.accepted.Load().Height(); height <= accepted {
		return fmt.Errorf("%w at height %d <= last-accepted (%d)", errBlockHeightTooLow, height, accepted)
	}

	rebuilt, err := vm.blockBuilder.rebuild(ctx, bCtx, parent, b)
	if err != nil {
		return err
	}
	// Although this is also checked in [blocks.Block.CopyAncestorsFrom], it is
	// key to the purpose of this method so included here to be defensive. It
	// also provides a clearer failure message.
	if reH, verH := rebuilt.Hash(), b.Hash(); reH != verH {
		vm.log().Debug("block verification failed",
			zap.Reflect("block", b.Header()),
			zap.Reflect("rebuilt", rebuilt.Header()),
		)
		return fmt.Errorf("%w; rebuilt as %#x when verifying %#x", errHashMismatch, reH, verH)
	}
	if err := b.CopyAncestorsFrom(rebuilt); err != nil {
		return err
	}
	b.SetWorstCaseBounds(rebuilt.WorstCaseBounds())

	vm.consensusCritical.Store(b.Hash(), b)
	return nil
}

func canonicalBlock(db ethdb.Database, num uint64) (*types.Block, error) {
	b := rawdb.ReadBlock(db, rawdb.ReadCanonicalHash(db, num), num)
	if b == nil {
		return nil, fmt.Errorf("no canonical block at height %d", num)
	}
	return b, nil
}

func (vm *VM) settledBlockFromDB(db ethdb.Reader, hash common.Hash, num uint64) (*blocks.Block, error) {
	// Before doing any disk IO, we sanity check that num is for a settled
	// block.
	//
	// If using this function with [readByHash] this check is required.
	// Otherwise, there is a possible (read: near impossible but non-zero)
	// chance that [VM.VerifyBlock] and [VM.AcceptBlock] were *both* called
	// between checking the in-memory block store and loading the canonical
	// number from the database. That could result in attempting to restore an
	// unexecuted block, which would report an error.
	//
	// TODO(arr4n) I think [readHash] should be providing this guarantee
	// as it has access to the [syncMap] and its lock.
	if vm.last.settled.Load().Height() < num {
		return nil, database.ErrNotFound
	}

	ethB := rawdb.ReadBlock(db, hash, num)
	if num > vm.last.synchronous {
		return blocks.RestoreSettledBlock(
			ethB,
			vm.log(),
			vm.db,
			vm.xdb,
			vm.exec.ChainConfig(),
		)
	}

	b, err := vm.blockBuilder.new(ethB, nil, nil)
	if err != nil {
		return nil, err
	}
	// Excess is only used for executing the next block, which can never
	// be the case if `b` isn't actually the last synchronous block, so
	// passing the same value for all is OK.
	if err := b.MarkSynchronous(vm.hooks, vm.db, vm.xdb, vm.config.ExcessAfterLastSynchronous); err != nil {
		return nil, err
	}
	return b, nil
}

// GetBlock returns the block with the given ID, or [database.ErrNotFound].
//
// It is expected that blocks that have been successfully verified should be
// returned correctly. It is also expected that blocks that have been
// accepted by the consensus engine should be able to be fetched. It is not
// required for blocks that have been rejected by the consensus engine to be
// able to be fetched.
func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (*blocks.Block, error) {
	var _ snowman.Block // protect the input to allow comment linking

	b, err := blocks.FromHash(
		vm.chain(),
		common.Hash(id),
		false, // consensus MAY request verified-but-not-accepted blocks
		func(b *blocks.Block) *blocks.Block {
			return b
		},
		vm.settledBlockFromDB,
	)
	if errors.Is(err, blocks.ErrNotFound) {
		return nil, database.ErrNotFound
	}
	return b, nil
}

// GetBlockIDAtHeight returns the accepted block at the given height, or
// [database.ErrNotFound].
func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	id := ids.ID(rawdb.ReadCanonicalHash(vm.db, height))
	if id == ids.Empty {
		return id, database.ErrNotFound
	}
	return id, nil
}

var (
	_ saetypes.BlockSource  = (*VM)(nil).ethBlockSource
	_ saetypes.HeaderSource = (*VM)(nil).headerSource
)

func (vm *VM) ethBlockSource(hash common.Hash, num uint64) (*types.Block, bool) {
	return source(vm, hash, num, (*blocks.Block).EthBlock, rawdb.ReadBlock)
}

func (vm *VM) headerSource(hash common.Hash, num uint64) (*types.Header, bool) {
	return source(vm, hash, num, (*blocks.Block).Header, rawdb.ReadHeader)
}

func source[T any](vm *VM, hash common.Hash, num uint64, fromMem blocks.Extractor[T], fromDB blocks.DBReader[T]) (*T, bool) {
	if b, ok := vm.consensusCritical.Load(hash); ok {
		if b.NumberU64() != num {
			return nil, false
		}
		return fromMem(b), true
	}
	x := fromDB(vm.db, hash, num)
	return x, x != nil
}
