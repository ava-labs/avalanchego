// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"

	atomicstate "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
)

const (
	atomicDynamicSyncerName = "Atomic State Syncer (dynamic)"
	atomicDynamicSyncerID   = "state_atomic_sync"
)

var _ types.PivotSession = (*atomicPivotSession)(nil)

// atomicPivotSession implements types.PivotSession for atomic trie sync.
type atomicPivotSession struct {
	inner      *Syncer
	client     types.LeafClient
	db         *versiondb.Database
	atomicTrie *atomicstate.AtomicTrie
	opts       []SyncerOption
}

func (s *atomicPivotSession) Run(ctx context.Context) error {
	return s.inner.Sync(ctx)
}

func (*atomicPivotSession) ShouldPivot(common.Hash) bool {
	// Always returns true because the incoming root is a block root
	// (proxy), not the actual atomic root, so comparing it is meaningless.
	return true
}

func (s *atomicPivotSession) Rebuild(newRoot common.Hash, newHeight uint64) (types.PivotSession, error) {
	log.Info("atomic syncer pivoting", "newHeight", newHeight, "newRoot", newRoot)

	if err := s.inner.Finalize(); err != nil {
		log.Error("failed to flush atomic syncer during pivot", "err", err)
	}
	s.atomicTrie.ResetToLastCommitted()

	newInner, err := NewSyncer(s.client, s.db, s.atomicTrie, newRoot, newHeight, s.opts...)
	if err != nil {
		return nil, err
	}
	return &atomicPivotSession{
		inner:      newInner,
		client:     s.client,
		db:         s.db,
		atomicTrie: s.atomicTrie,
		opts:       s.opts,
	}, nil
}

func (*atomicPivotSession) OnSessionComplete() error {
	return nil
}

// Finalize flushes the inner syncer's in-progress work.
func (s *atomicPivotSession) Finalize() error {
	return s.inner.Finalize()
}

// NewAtomicDynamicSyncer creates a DynamicSyncer backed by an
// atomicPivotSession. The returned syncer supports pivoting to new
// targets during sync.
func NewAtomicDynamicSyncer(
	inner *Syncer,
	client types.LeafClient,
	db *versiondb.Database,
	atomicTrie *atomicstate.AtomicTrie,
	initialRoot common.Hash,
	initialHeight uint64,
	opts ...SyncerOption,
) *types.DynamicSyncer {
	session := &atomicPivotSession{
		inner:      inner,
		client:     client,
		db:         db,
		atomicTrie: atomicTrie,
		opts:       opts,
	}
	return types.NewDynamicSyncer(
		atomicDynamicSyncerName,
		atomicDynamicSyncerID,
		session,
		initialRoot,
		initialHeight,
	)
}
