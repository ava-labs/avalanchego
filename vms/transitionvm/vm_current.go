// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (v *VM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.AppGossip(ctx, nodeID, msg)
}

func (v *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.AppRequest(ctx, nodeID, requestID, deadline, request)
}

func (v *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.HealthCheck(ctx)
}

func (v *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.LastAccepted(ctx)
}

func (v *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.GetBlockIDAtHeight(ctx, height)
}

func (v *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.SetPreference(ctx, blkID)
}

func (v *VM) SetPreferenceWithContext(ctx context.Context, blkID ids.ID, blockCtx *block.Context) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.SetPreferenceWithContext(ctx, blkID, blockCtx)
}

func (v *VM) Version(ctx context.Context) (string, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.Version(ctx)
}

func (v *VM) Shutdown(ctx context.Context) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.Shutdown(ctx)
}

var _ block.StateSyncableVM = (*VM)(nil)

func (v *VM) StateSyncEnabled(ctx context.Context) (bool, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.StateSyncEnabled(ctx)
}

func (v *VM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.GetOngoingSyncStateSummary(ctx)
}

func (v *VM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.GetLastStateSummary(ctx)
}

func (v *VM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.ParseStateSummary(ctx, summaryBytes)
}

func (v *VM) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.GetStateSummary(ctx, summaryHeight)
}
