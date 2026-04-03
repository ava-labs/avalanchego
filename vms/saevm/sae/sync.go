// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

/*
Bootstrapping situations
With state sync
- if no state, state sync, start up from that block
- if far enough back ???

Without state sync
-
*/

/*
Needed during sync:
- LastAccepted
- GetBlock
- Everything in common.VM
- block.StateSyncableVM
*/

// StateSyncEnabled TODO.
func (vm *VM) StateSyncEnabled(context.Context) (bool, error) {
	return vm.config.StateSyncEnabled, nil
}

// GetOngoingSyncStateSummary implements [block.StateSyncableVM].
// TODO(alarso16): allow resume
func (vm *VM) GetOngoingSyncStateSummary(context.Context) (*stateSummary, error) {
	return nil, database.ErrNotFound
}

// GetLastStateSummary implements [block.StateSyncableVM].
func (vm *VM) GetLastStateSummary(ctx context.Context) (*stateSummary, error) {
	height := saedb.LastCommittedTrieDBHeight(vm.exec.LastExecuted().NumberU64(), vm.config.DBConfig.CommitInterval())
	if height < vm.last.synchronous {
		return vm.GetStateSummary(ctx, vm.last.synchronous)
	}
	return vm.GetStateSummary(ctx, height)
}

// GetStateSummary implements [block.StateSyncableVM].
func (vm *VM) GetStateSummary(ctx context.Context, height uint64) (*stateSummary, error) {
	if height < vm.last.synchronous {
		return nil, fmt.Errorf("cannot sync to before SAE")
	}
	hash := rawdb.ReadCanonicalHash(vm.db, height)
	b := rawdb.ReadBlock(vm.db, hash, height)
	if b == nil {
		return nil, fmt.Errorf("%w: block at height %d", database.ErrNotFound, height)
	}

	if _, err := vm.exec.StateDB(b.Root()); err != nil {
		return nil, fmt.Errorf("state %+x:%d unavailable", b.Root(), height)
	}

	return &stateSummary{
		block:           b,
		lastSynchronous: vm.last.synchronous,
	}, nil
}

// ParseStateSummary implements [block.StateSyncableVM].
// TODO(alarso16): find a better way to encode this.
func (vm *VM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (*stateSummary, error) {
	if len(summaryBytes) < 8 {
		return nil, fmt.Errorf("summary too short: %d bytes", len(summaryBytes))
	}
	lastSynchronous := binary.LittleEndian.Uint64(summaryBytes[len(summaryBytes)-8:])
	var block types.Block
	if err := rlp.DecodeBytes(summaryBytes[:len(summaryBytes)-8], &block); err != nil {
		return nil, fmt.Errorf("decode block: %w", err)
	}
	return &stateSummary{block: &block, lastSynchronous: lastSynchronous}, nil
}

var _ adaptor.SummaryProperties = (*stateSummary)(nil)

type stateSummary struct {
	block           *types.Block
	lastSynchronous uint64
}

// AcceptSync move to hooks
// We assume the creator has access to:
// - [ethdb.Database]
// - [hook.Points]
// - snow context
// - networking
//
// Should request
// - settled block + ??? blocks for block hash retrieval in execution
// - settled state and code

// AcceptSummary initiates the state sync in the background.
func (vm *VM) AcceptSummary(ctx context.Context, ss *stateSummary) (block.StateSyncMode, error) {
	// any reason to bail now?
	// e.g. not that far behind, resume previous sync

	var (
		wgCtx, cancel = context.WithCancel(ctx)
		wg            sync.WaitGroup
	)
	wg.Go(func() {
		var err error
		defer func(doneCh chan error) {
			doneCh <- err
		}(vm.stateSyncDone)
		if err = vm.hooks.StateSync(wgCtx, ss.block, hook.StateSyncConfig{
			DB:      vm.db,
			SnowCtx: vm.snowCtx,
			Network: vm.Network,
			Peers:   vm.Peers,
		}); err != nil {
			// handle err
			return
		}
		// Start VM from state sync mode
		// Inform engine of state sync complete
		vm.last.synchronous = ss.lastSynchronous

		// TODO(alarso16): bloom indexer garbage
		ethB, err := canonicalBlock(vm.db, vm.hooks.SettledHeight(ss.block.Header()))
		if err != nil {
			return
		}

		settled, err := blocks.New(ethB, nil, nil, vm.snowCtx.Log)
		if err != nil {
			return
		}

		if err = settled.MarkSyncedWith(ss.block.Header(), vm.hooks, vm.db, vm.xdb); err != nil {
			return
		}

		if err = vm.afterSync(wgCtx, settled); err != nil {
			return
		}
	})

	vm.toClose = append(vm.toClose, closerFunc(func() error {
		cancel()
		wg.Wait()
		return nil
	}))

	return block.StateSyncStatic, nil
}

// Bytes returns an encoding of the state summary
//
// TODO: Is there a better way to encode this?
func (s *stateSummary) Bytes() []byte {
	b, err := rlp.EncodeToBytes(s.block)
	if err != nil {
		panic("unexpected decode err" + err.Error())
	}

	i := make([]byte, 8)
	binary.LittleEndian.PutUint64(i, s.lastSynchronous)

	return append(b, i...)
}

// Height return the block noted in the state summary.
func (s *stateSummary) Height() uint64 {
	return s.block.NumberU64()
}

// ID implements [block.StateSummary].
func (s *stateSummary) ID() ids.ID {
	return ids.ID(s.block.Hash())
}
