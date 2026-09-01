// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"fmt"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/graft/evm/sync/leaf"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"
)

// RegisterSyncHandler returns a [p2p.Handler] that serves atomic trie leaves to
// a [Syncer], counting the served requests on reg.
func RegisterSyncHandler(n *p2p.Network, state *State, reg prometheus.Registerer) error {
	return hashdb.RegisterHandler(state.snowCtx.Log, n, p2p.EVMAtomicLeafRequestHandlerID, state.trieDB, keyLength, reg)
}

// Syncer is a [leaf.CallbackSyncer] that can fetch and apply the atomic trie to
// a [State] and update shared memory.
type Syncer struct {
	syncer *leaf.CallbackSyncer

	state        *State
	targetRoot   common.Hash
	targetHeight uint64
}

// NewSyncer creates a new atomic syncer, counting its requests on m. The
// syncer will start with a call to [Syncer.Sync].
func NewSyncer(n *p2p.Network, pt *p2p.PeerTracker, state *State, root common.Hash, height uint64, m *network.Metrics) *Syncer {
	const requestSize = 1024

	tasks := make(chan leaf.SyncTask, 1)
	// If the trie already has the target root there is nothing to fetch.
	if state.currentRoot != root {
		tasks <- &task{
			state:      state,
			targetRoot: root,
			start:      firstKeyAfterHeight(state.currentHeight.Load()),
			pendingOps: make(map[ids.ID]*atomic.Requests),
		}
	}
	close(tasks) // no more tasks will be sent

	return &Syncer{
		syncer: leaf.NewCallbackSyncer(
			hashdb.NewClient(
				state.snowCtx.Log,
				n,
				p2p.EVMAtomicLeafRequestHandlerID,
				keyLength,
				pt,
				m,
			),
			tasks,
			&leaf.SyncerConfig{
				RequestSize: requestSize,
				NumWorkers:  1,
			},
		),
		targetRoot:   root,
		targetHeight: height,
		state:        state,
	}
}

// Sync fetches the atomic trie from a peer and applies it to the state,
// updating shared memory as it goes. Any error MUST be treated as fatal.
func (s *Syncer) Sync(ctx context.Context) error {
	if err := s.syncer.Sync(ctx); err != nil {
		return fmt.Errorf("syncing atomic trie: %w", err)
	}

	if s.state.currentRoot != s.targetRoot {
		return fmt.Errorf("synced root (%s) does not match target (%s) for atomic trie", s.state.currentRoot, s.targetRoot)
	}

	// Update the shared memory markers to tip, since we have the most recent state
	// The recent blocks may not have had any atomic txs, so it wouldn't have been updated in [syncTask.OnFinish].
	if s.state.currentHeight.Load() < s.targetHeight {
		if err := s.state.writeToSharedMemory(s.state.db.NewBatch(), s.targetHeight, s.targetRoot, nil); err != nil {
			return fmt.Errorf("committing synced height %d: %w", s.targetHeight, err)
		}
	}
	return nil
}

// firstKeyAfterHeight returns the first trie key that would need synced, assuming all
// state up to currentHeight is already available.
func firstKeyAfterHeight(currentHeight uint64) []byte {
	if currentHeight == 0 {
		return nil // need entire trie
	}
	return encodeTrieKey(currentHeight+1, ids.Empty)
}

var _ leaf.SyncTask = (*task)(nil)

// task is supplied to the leaf syncer, tracking the pending state for the sync.
type task struct {
	state      *State
	targetRoot common.Hash
	start      []byte

	// pending accumulates the current height's leaves until a height boundary, at
	// which point they are committed together.
	pendingHeight uint64
	pendingKeys   [][]byte
	pendingVals   [][]byte
	pendingOps    map[ids.ID]*atomic.Requests
}

func (*task) OnStart() (skip bool, _ error) { return false, nil }

func (t *task) Root() common.Hash  { return t.targetRoot }
func (t *task) Start() []byte      { return t.start }
func (*task) End() []byte          { return nil }
func (*task) Account() common.Hash { return common.Hash{} }

// OnLeaves is called on each batch from the [leaf.Syncer]. All state is queued
// to be committed for each individual height. Any error returned will be
// treated as fatal.
func (t *task) OnLeafs(_ context.Context, keys, vals [][]byte) error {
	for i, key := range keys {
		if len(key) != keyLength {
			return fmt.Errorf("unexpected trie key length %d, expected %d", len(key), keyLength)
		}
		height, chainID := decodeTrieKey(key)

		// A new height means all of the previous height's leaves have arrived, so
		// it can be committed before accumulating this one.
		if height != t.pendingHeight {
			if err := t.flush(); err != nil {
				return err
			}
		}

		req := new(atomic.Requests)
		if _, err := c.Unmarshal(vals[i], req); err != nil {
			return fmt.Errorf("unmarshaling atomic requests for chain %s: %w", chainID, err)
		}

		t.pendingHeight = height
		t.pendingKeys = append(t.pendingKeys, slices.Clone(key))
		t.pendingVals = append(t.pendingVals, slices.Clone(vals[i]))
		mergeRequests(t.pendingOps, chainID, req)
	}

	return nil
}

// OnFinish is called after the entire remote trie has been included in
// [task.OnLeaves]. Any remaining leaves are pushed to disk, as the last
// block with an atomic op.
func (t *task) OnFinish(context.Context) error {
	return t.flush()
}

// flush inserts the accumulated height's leaves into the trie, then commits the
// resulting root and the height's shared memory atomically, and resets the
// pending buffers. Leaf sync only delivers heights above the committed tip, so
// the height is never already applied.
func (t *task) flush() error {
	if len(t.pendingKeys) == 0 {
		return nil
	}
	newRoot, err := applyTrie(t.state.trieDB, t.state.currentRoot, t.pendingKeys, t.pendingVals)
	if err != nil {
		return fmt.Errorf("applying synced trie at height %d: %w", t.pendingHeight, err)
	}
	if err := t.state.writeToSharedMemory(t.state.db.NewBatch(), t.pendingHeight, newRoot, t.pendingOps); err != nil {
		return fmt.Errorf("committing synced height %d: %w", t.pendingHeight, err)
	}

	t.pendingKeys = nil
	t.pendingVals = nil
	t.pendingOps = make(map[ids.ID]*atomic.Requests)
	return nil
}
