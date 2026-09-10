// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// ParseBlock parses the buffer via [blocks.ParseEth]. It does NOT populate the
// block ancestry, which is done by [VM.VerifyBlock] i.f.f. verification
// passes.
func (vm *VM) ParseBlock(ctx context.Context, buf []byte) (*blocks.Block, error) {
	b, err := blocks.ParseEth(buf, vm.hooks)
	if err != nil {
		return nil, err
	}

	return vm.blockBuilder.new(b, nil, nil)
}

// BuildBlock builds a new block, using the last block passed to
// [VM.SetPreference] as the parent. The block context MAY be nil.
func (vm *VM) BuildBlock(ctx context.Context, bCtx *block.Context) (*blocks.Block, error) {
	return vm.blockBuilder.build(ctx, bCtx, vm.preference.Load())
}

// saeparams.MaxBlockBytes < constants.DefaultMaxMessageSize
const _ uint = constants.DefaultMaxMessageSize - saeparams.MaxBlockBytes - 1

var (
	errUnknownParent     = errors.New("unknown parent")
	errBlockHeightTooLow = errors.New("block height too low")
	errHashMismatch      = errors.New("hash mismatch")
	errBlockTooLarge     = errors.New("block size exceeds maximum")
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

	if vm.consensusState.Get() == snow.Bootstrapping {
		return vm.verifyWhenBootstrapping(b, parent)
	}

	if size := b.EthBlock().Size(); size > saeparams.MaxBlockBytes {
		return fmt.Errorf("%w: %d > %d bytes", errBlockTooLarge, size, saeparams.MaxBlockBytes)
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

var (
	errSettledRootMismatch   = errors.New("settled root mismatch")
	errSettledHeightMismatch = errors.New("settled height mismatch")
)

// verifyWhenBootstrapping skips verification in its entirety. It is expected
// for blocks to be verified by hash in the bootstrapping engine. This supports
// hooks, such as Coreth and Subnet-EVM, that are unable to fully verify blocks
// during bootstrapping.
func (vm *VM) verifyWhenBootstrapping(b, parent *blocks.Block) error {
	header := b.Header()
	lastSettled, err := lastToSettle(vm.hooks, header, parent, vm.config.Now(), vm.log())
	if err != nil {
		return err
	}

	// Sanity checks to ensure the in-memory settled block matches the expected
	// settled block.
	if got, want := lastSettled.PostExecutionStateRoot(), b.SettledStateRoot(); got != want {
		return fmt.Errorf("%w: got %#x ; want %#x", errSettledRootMismatch, got, want)
	}
	if got, want := lastSettled.NumberU64(), vm.hooks.SettledBy(header).Height; got != want {
		return fmt.Errorf("%w: got %d ; want %d", errSettledHeightMismatch, got, want)
	}
	if err := b.SetAncestors(parent, lastSettled); err != nil {
		return err
	}

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
	if ethB == nil {
		return nil, database.ErrNotFound
	}

	return blocks.RestoreSettledBlock(
		ethB,
		vm.hooks,
		vm.log(),
		vm.db,
		vm.xdb,
		vm.exec.ChainConfig(),
	)
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
	return b, err
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

func ethBlockSource(m *syncMap[common.Hash, *blocks.Block], db ethdb.Database) saetypes.BlockSource {
	return func(hash common.Hash, num uint64) (*types.Block, bool) {
		return source(m, db, hash, num, (*blocks.Block).EthBlock, rawdb.ReadBlock)
	}
}

func headerSource(m *syncMap[common.Hash, *blocks.Block], db ethdb.Database) saetypes.HeaderSource {
	return func(hash common.Hash, num uint64) (*types.Header, bool) {
		return source(m, db, hash, num, (*blocks.Block).Header, rawdb.ReadHeader)
	}
}

func source[T any](cc *syncMap[common.Hash, *blocks.Block], db ethdb.Database, hash common.Hash, num uint64, fromMem blocks.Extractor[T], fromDB blocks.DBReader[T]) (*T, bool) {
	if b, ok := cc.Load(hash); ok {
		if b.NumberU64() != num {
			return nil, false
		}
		return fromMem(b), true
	}
	x := fromDB(db, hash, num)
	return x, x != nil
}
