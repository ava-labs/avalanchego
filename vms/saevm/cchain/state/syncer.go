// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/leaf"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/chains/atomic"
	corethsync "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/sync"
)

// NewSyncHandler returns a [p2p.Handler] that serves atomic trie leaves to
// a [Syncer]. This handler MUST be registered to the network at the
// [p2p.EVMAtomicLeafRequestHandlerID] handler ID.
func NewSyncHandler(state *State, log logging.Logger) p2p.Handler {
	return hashdb.NewHandler(log, hashdb.NewResponder(state.trieDB, keyLength, nil /*snapshot*/))
}

// Syncer is a [leaf.LeafSyncer] that can fetch and apply the atomic trie to a
// [State] and update shared memory.
type Syncer struct {
	leafSyncer *leaf.CallbackSyncer

	state        *State
	targetRoot   common.Hash
	targetHeight uint64
}

// NewSyncer creates a new atomic syncer. The syncer will start with a call to [Syncer.Sync].
func NewSyncer(n *p2p.Network, pt *p2p.PeerTracker, state *State, log logging.Logger, root common.Hash, height uint64) (*Syncer, error) {
	s := &Syncer{
		targetRoot:   root,
		targetHeight: height,
		state:        state,
	}

	// Since the trie is append-only, with keys based on block height, there is
	// no reason to parallelize.
	tasks := make(chan leaf.SyncTask, 1)
	tasks <- &syncTask{
		state:      state,
		targetRoot: root,
		start:      firstKeyAfterHeight(state.currentHeight.Load()),
		pendingOps: make(map[ids.ID]*atomic.Requests),
	}
	close(tasks) // no more tasks will be sent

	s.leafSyncer = leaf.NewCallbackSyncer(
		hashdb.NewGetter(n, pt, p2p.EVMAtomicLeafRequestHandlerID, log),
		tasks,
		&leaf.SyncerConfig{
			NumWorkers:  1,
			RequestSize: hashdb.MaxLeavesLimit,
		},
	)
	return s, nil
}

// Sync fetches the atomic trie from a peer and applies it to the state,
// updating shared memory as it goes. Any error MUST be treated as fatal.
func (s *Syncer) Sync(ctx context.Context) error {
	if err := s.leafSyncer.Sync(ctx); err != nil {
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
	start := make([]byte, keyLength)
	binary.BigEndian.PutUint64(start, currentHeight+1)
	return start
}

var _ leaf.SyncTask = (*syncTask)(nil)

// syncTask is supplied to the leaf syncer, tracking the pending state for the sync.
type syncTask struct {
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

func (t *syncTask) Root() common.Hash        { return t.targetRoot }
func (t *syncTask) Start() []byte            { return t.start }
func (*syncTask) End() []byte                { return nil }
func (*syncTask) NodeType() message.NodeType { return corethsync.TrieNode }
func (*syncTask) Account() common.Hash       { return common.Hash{} }

// OnStart returns true if the trie already has the correct state root.
func (t *syncTask) OnStart() (canSkip bool, _ error) {
	return t.state.currentRoot == t.targetRoot, nil
}

// OnLeafs is called on each request from the [leaf.LeafSyncer]. All state is
// queued to be committed for each individual height. Any error returned will
// be treated as fatal.
func (t *syncTask) OnLeafs(_ context.Context, keys [][]byte, vals [][]byte) error {
	for i, key := range keys {
		if len(key) != keyLength {
			return fmt.Errorf("unexpected trie key length %d, expected %d", len(key), keyLength)
		}
		height := binary.BigEndian.Uint64(key[:wrappers.LongLen])
		chainID := ids.ID(key[wrappers.LongLen:])

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
// [syncTask.OnLeafs]. Any remaining leaves are pushed to disk, as the last
// block with an atomic op.
func (t *syncTask) OnFinish(context.Context) error {
	return t.flush()
}

// flush inserts the accumulated height's leaves into the trie, then commits the
// resulting root and the height's shared memory atomically, and resets the
// pending buffers. Leaf sync only delivers heights above the committed tip, so
// the height is never already applied.
func (t *syncTask) flush() error {
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
