// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

type FullVM interface {
	StateSyncableVM
	BatchedChainVM
	ChainVM
}

type ChangeNotifier struct {
	ChainVM
	OnChange func()
}

func (cn *ChangeNotifier) GetAncestors(ctx context.Context, blkID ids.ID, maxBlocksNum int, maxBlocksSize int, maxBlocksRetrivalTime time.Duration) ([][]byte, error) {
	if batchedVM, ok := cn.ChainVM.(BatchedChainVM); ok {
		return batchedVM.GetAncestors(ctx, blkID, maxBlocksNum, maxBlocksSize, maxBlocksRetrivalTime)
	}
	return nil, ErrRemoteVMNotImplemented
}

func (cn *ChangeNotifier) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]snowman.Block, error) {
	if batchedVM, ok := cn.ChainVM.(BatchedChainVM); ok {
		return batchedVM.BatchedParseBlock(ctx, blks)
	}
	return nil, ErrRemoteVMNotImplemented
}

func (cn *ChangeNotifier) StateSyncEnabled(ctx context.Context) (bool, error) {
	if ssVM, isSSVM := cn.ChainVM.(StateSyncableVM); isSSVM {
		return ssVM.StateSyncEnabled(ctx)
	}
	return false, nil
}

func (cn *ChangeNotifier) GetOngoingSyncStateSummary(ctx context.Context) (StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(StateSyncableVM); isSSVM {
		return ssVM.GetOngoingSyncStateSummary(ctx)
	}
	return nil, ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) GetLastStateSummary(ctx context.Context) (StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(StateSyncableVM); isSSVM {
		return ssVM.GetLastStateSummary(ctx)
	}
	return nil, ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) ParseStateSummary(ctx context.Context, summaryBytes []byte) (StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(StateSyncableVM); isSSVM {
		return ssVM.ParseStateSummary(ctx, summaryBytes)
	}
	return nil, ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) GetStateSummary(ctx context.Context, summaryHeight uint64) (StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(StateSyncableVM); isSSVM {
		return ssVM.GetStateSummary(ctx, summaryHeight)
	}
	return nil, ErrStateSyncableVMNotImplemented
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
