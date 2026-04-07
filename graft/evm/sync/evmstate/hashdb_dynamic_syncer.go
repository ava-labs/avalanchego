// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

const hashDBDynamicSyncerName = "HashDB EVM State Syncer (dynamic)"

var _ types.PivotSession = (*hashDBPivotSession)(nil)

// hashDBPivotSession implements types.PivotSession for EVM state sync with HashDB.
type hashDBPivotSession struct {
	inner            *HashDBSyncer
	syncClient       client.Client
	db               ethdb.Database
	leafsRequestSize uint16
	leafsRequestType message.LeafsRequestType
	opts             []HashDBSyncerOption
	codeQueue        *code.SessionedQueue
}

func (s *hashDBPivotSession) Run(ctx context.Context) error {
	return s.inner.Sync(ctx)
}

func (s *hashDBPivotSession) ShouldPivot(newRoot common.Hash) bool {
	return newRoot != s.inner.root
}

func (s *hashDBPivotSession) Rebuild(newRoot common.Hash, _ uint64) (types.PivotSession, error) {
	log.Info("state syncer pivoting to new root", "oldRoot", s.inner.root, "newRoot", newRoot)

	if _, _, err := s.codeQueue.PivotTo(newRoot); err != nil {
		return nil, fmt.Errorf("failed to pivot code queue: %w", err)
	}
	if err := s.inner.Finalize(); err != nil {
		log.Error("failed to flush in-progress batches during pivot", "err", err)
	}
	<-snapshot.WipeSnapshot(s.db, false)

	newInner, err := NewHashDBSyncer(s.syncClient, s.db, newRoot, s.codeQueue, s.leafsRequestSize, s.leafsRequestType, s.opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create syncer for root %s: %w", newRoot, err)
	}
	return &hashDBPivotSession{
		inner:            newInner,
		syncClient:       s.syncClient,
		db:               s.db,
		leafsRequestSize: s.leafsRequestSize,
		leafsRequestType: s.leafsRequestType,
		opts:             s.opts,
		codeQueue:        s.codeQueue,
	}, nil
}

func (s *hashDBPivotSession) OnSessionComplete() error {
	return s.codeQueue.Finalize()
}

// Finalize flushes the inner syncer's in-progress work.
func (s *hashDBPivotSession) Finalize() error {
	return s.inner.Finalize()
}

// NewHashDBDynamicSyncer creates a state syncer that supports pivoting to a new
// root mid-sync via UpdateTarget. It returns a DynamicSyncer backed by a
// hashDBPivotSession.
func NewHashDBDynamicSyncer(syncClient client.Client, db ethdb.Database, root common.Hash, codeQueue *code.SessionedQueue, leafsRequestSize uint16, leafsRequestType message.LeafsRequestType, opts ...HashDBSyncerOption) (*types.DynamicSyncer, error) {
	inner, err := NewHashDBSyncer(syncClient, db, root, codeQueue, leafsRequestSize, leafsRequestType, opts...)
	if err != nil {
		return nil, err
	}
	if _, err := codeQueue.Start(root); err != nil {
		return nil, err
	}

	session := &hashDBPivotSession{
		inner:            inner,
		syncClient:       syncClient,
		db:               db,
		leafsRequestSize: leafsRequestSize,
		leafsRequestType: leafsRequestType,
		opts:             opts,
		codeQueue:        codeQueue,
	}
	return types.NewDynamicSyncer(
		hashDBDynamicSyncerName,
		StateSyncerID,
		session,
		root,
		0,
	), nil
}
