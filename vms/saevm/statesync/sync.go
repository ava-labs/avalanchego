// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
)

// StateSyncEnabled implements [adaptor.SyncVM].
func (vm *VM[_]) StateSyncEnabled(context.Context) (bool, error) {
	return vm.cfg.StateSyncEnabled, nil
}

// AcceptSummary initiates the state sync in the background.
func (vm *VM[_]) AcceptSummary(ctx context.Context, ss *Summary) (block.StateSyncMode, error) {
	// any reason to bail now?
	// e.g. not that far behind, resume previous sync

	var (
		// wgCtx, cancel = context.WithCancel(ctx)
		wg sync.WaitGroup
	)
	wg.Go(func() {
		defer close(vm.stateSyncDone)

		if err := vm.doStateSync(ctx, ss); err != nil {
			panic(err)
		}

		// TODO(alarso16):
		// - bloom indexer garbage
		// - mark execution results for settled block
		// - report any error gracefully

		vm.initInnerVM(ctx)
	})

	// TODO: allow shutdown

	return block.StateSyncStatic, nil
}

func (vm *VM[_]) doStateSync(ctx context.Context, ss *Summary) error {
	{
		blockSyncer, err := newBlockSyncer(vm.Network, vm.db, ss.block, vm.hooks.SettledBy)
		if err != nil {
			return err
		}
		if err := blockSyncer.Sync(ctx); err != nil {
			return err
		}
	}
	{
		settledStateRoot := ss.block.Root()
		stateSyncer, err := newStateSyncer(vm.Network, vm.cfg.Config.DBConfig, vm.db, vm.snowCtx, settledStateRoot)
		if err != nil {
			return err
		}
		if err := stateSyncer.Sync(ctx); err != nil {
			return err
		}
	}
	return nil
}

// GetOngoingSyncStateSummary implements [adaptor.SyncVM].
func (vm *VM[_]) GetOngoingSyncStateSummary(context.Context) (*Summary, error) {
	// TODO(alarso16): track ongoing sync summary
	// Unnecessary for now, we can just restart, who cares
	return nil, database.ErrNotFound
}

// stubs to be implemented later, but required to compile for now

type syncer interface {
	Sync(ctx context.Context) error
}

func newBlockSyncer(
	network sae.Network,
	db ethdb.Database,
	summaryBlock *types.Block,
	settledPoints func(*types.Header) hook.Settled,
) (syncer, error) {
	// get block hash
	// calc number of blocks to fetch
	// create syncer and return
	panic("unimplemented")
}

func newStateSyncer(
	network sae.Network,
	dbConfig saedb.Config,
	db ethdb.Database,
	snowCtx *snow.Context,
	root common.Hash,
) (syncer, error) {
	// this one is complicated.
	// 1. Set up all p2p handlers based on config
	// 2. Start code syncer
	// 3. Start trie syncer (either hashdb or firewood)
	// 4. Wait for both to complete
	// 5. Get atomic root from state, start atomic syncer
	// 6. wait to finish
	panic("unimplemented")
}
