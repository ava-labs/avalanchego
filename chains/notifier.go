// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type ChangeNotifier struct {
	block.ChainVM
	OnChange func()
}

func (cn *ChangeNotifier) GetAncestors(ctx context.Context, blkID ids.ID, maxBlocksNum int, maxBlocksSize int, maxBlocksRetrivalTime time.Duration) ([][]byte, error) {
	if batchedVM, ok := cn.ChainVM.(block.BatchedChainVM); ok {
		return batchedVM.GetAncestors(ctx, blkID, maxBlocksNum, maxBlocksSize, maxBlocksRetrivalTime)
	}
	return nil, block.ErrRemoteVMNotImplemented
}

func (cn *ChangeNotifier) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]snowman.Block, error) {
	if batchedVM, ok := cn.ChainVM.(block.BatchedChainVM); ok {
		return batchedVM.BatchedParseBlock(ctx, blks)
	}
	return nil, block.ErrRemoteVMNotImplemented
}

func (cn *ChangeNotifier) StateSyncEnabled(ctx context.Context) (bool, error) {
	if ssVM, isSSVM := cn.ChainVM.(block.StateSyncableVM); isSSVM {
		return ssVM.StateSyncEnabled(ctx)
	}
	return false, nil
}

func (cn *ChangeNotifier) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(block.StateSyncableVM); isSSVM {
		return ssVM.GetOngoingSyncStateSummary(ctx)
	}
	return nil, block.ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(block.StateSyncableVM); isSSVM {
		return ssVM.GetLastStateSummary(ctx)
	}
	return nil, block.ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(block.StateSyncableVM); isSSVM {
		return ssVM.ParseStateSummary(ctx, summaryBytes)
	}
	return nil, block.ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(block.StateSyncableVM); isSSVM {
		return ssVM.GetStateSummary(ctx, summaryHeight)
	}
	return nil, block.ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) SetPreference(ctx context.Context, blkID ids.ID) error {
	defer cn.OnChange()
	return cn.ChainVM.SetPreference(ctx, blkID)
}

func (cn *ChangeNotifier) SetState(ctx context.Context, state snow.State) error {
	defer cn.OnChange()
	return cn.ChainVM.SetState(ctx, state)
}

func (cn *ChangeNotifier) BuildBlock(ctx context.Context) (snowman.Block, error) {
	defer cn.OnChange()
	return cn.ChainVM.BuildBlock(ctx)
}
