// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

const hashDBDynamicSyncerName = "HashDB EVM State Syncer (dynamic)"

var _ types.PivotSession = (*hashDBPivotSession)(nil)

// hashDBPivotSession implements types.PivotSession for EVM state sync with HashDB.
// It owns the code queue and code syncer, running both alongside the state
// syncer inside Run. On pivot, all three are shut down and rebuilt.
type hashDBPivotSession struct {
	inner      *HashDBSyncer
	codeQueue  *code.Queue
	codeSyncer *code.Syncer

	// Retained for rebuilding on pivot.
	syncClient       client.Client
	db               ethdb.Database
	leafsRequestSize uint16
	leafsRequestType message.LeafsRequestType
	baseOpts         []HashDBSyncerOption // excludes per-pivot incremental options
}

func (s *hashDBPivotSession) Run(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		log.Info("code syncer started", "root", s.inner.root)
		err := s.codeSyncer.Sync(egCtx)
		log.Info("code syncer finished", "root", s.inner.root, "err", err)
		return err
	})
	eg.Go(func() error {
		log.Info("state syncer started", "root", s.inner.root)
		err := s.inner.Sync(egCtx)
		log.Info("state syncer finished", "root", s.inner.root, "err", err)
		return err
	})
	return eg.Wait()
}

func (s *hashDBPivotSession) ShouldPivot(newRoot common.Hash) bool {
	return newRoot != s.inner.root
}

func (s *hashDBPivotSession) Rebuild(newRoot common.Hash, _ uint64) (types.PivotSession, error) {
	log.Info("state syncer pivoting to new root", "oldRoot", s.inner.root, "newRoot", newRoot)

	if err := s.inner.Finalize(); err != nil {
		log.Error("failed to flush in-progress batches during pivot", "err", err)
	}
	s.codeQueue.Shutdown()
	<-snapshot.WipeSnapshot(s.db, false)

	trieDB := triedb.NewDatabase(s.db, nil)
	incrementalOpts := []HashDBSyncerOption{
		WithPreserveSegments(),
		WithStorageTrieFilter(func(_ ethdb.Database, accountHash common.Hash, storageRoot common.Hash) bool {
			_, err := trie.New(trie.StorageTrieID(newRoot, storageRoot, accountHash), trieDB)
			return err == nil
		}),
	}
	return newHashDBPivotSession(s.syncClient, s.db, newRoot, s.leafsRequestSize, s.leafsRequestType, s.baseOpts, incrementalOpts)
}

func (*hashDBPivotSession) OnSessionComplete() error {
	// Code queue finalization is handled by the inner syncer's
	// onMainTrieFinished callback (via WithFinalizeCodeQueue).
	return nil
}

// Finalize flushes the inner syncer's in-progress work.
func (s *hashDBPivotSession) Finalize() error {
	return s.inner.Finalize()
}

func newHashDBPivotSession(syncClient client.Client, db ethdb.Database, root common.Hash, leafsRequestSize uint16, leafsRequestType message.LeafsRequestType, baseOpts []HashDBSyncerOption, extraOpts []HashDBSyncerOption) (*hashDBPivotSession, error) {
	codeQueue, err := code.NewQueue(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create code queue: %w", err)
	}
	codeSyncer, err := code.NewSyncer(syncClient, db, codeQueue.CodeHashes())
	if err != nil {
		return nil, fmt.Errorf("failed to create code syncer: %w", err)
	}

	allOpts := append(append(baseOpts, extraOpts...), WithFinalizeCodeQueue(codeQueue.Finalize))
	inner, err := NewHashDBSyncer(syncClient, db, root, codeQueue, leafsRequestSize, leafsRequestType, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create state syncer for root %s: %w", root, err)
	}
	return &hashDBPivotSession{
		inner:            inner,
		codeQueue:        codeQueue,
		codeSyncer:       codeSyncer,
		syncClient:       syncClient,
		db:               db,
		leafsRequestSize: leafsRequestSize,
		leafsRequestType: leafsRequestType,
		baseOpts:         baseOpts,
	}, nil
}

// NewHashDBDynamicSyncer creates a state syncer that supports pivoting to a new
// root mid-sync via UpdateTarget. The returned DynamicSyncer internally manages
// a code queue and code syncer per session.
func NewHashDBDynamicSyncer(syncClient client.Client, db ethdb.Database, root common.Hash, leafsRequestSize uint16, leafsRequestType message.LeafsRequestType, opts ...HashDBSyncerOption) (*types.DynamicSyncer, error) {
	session, err := newHashDBPivotSession(syncClient, db, root, leafsRequestSize, leafsRequestType, opts, nil)
	if err != nil {
		return nil, err
	}
	return types.NewDynamicSyncer(
		hashDBDynamicSyncerName,
		StateSyncerID,
		session,
		root,
		0,
	), nil
}
