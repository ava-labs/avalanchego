// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
)

// RegisterSyncHandler allows the [State] to serve its data to state-syncing
// peers.
func RegisterSyncHandler(n *p2p.Network, state *State) error {
	return hashdb.RegisterHandler(state.snowCtx.Log, n, p2p.EVMAtomicLeafRequestHandlerID, state.trieDB, keyLength)
}

// Syncer fetches the atomic trie from peers, applies it to a [State], and
// updates shared memory as it goes.
type Syncer struct {
	fetcher *hashdb.Client

	state        *State
	targetRoot   common.Hash
	targetHeight uint64
}

// NewSyncer creates a new atomic syncer. The syncer will start with a call to [Syncer.Sync].
func NewSyncer(n *p2p.Network, pt *p2p.PeerTracker, state *State, root common.Hash, height uint64) *Syncer {
	return &Syncer{
		fetcher: hashdb.NewClient(
			state.snowCtx.Log,
			n,
			p2p.EVMAtomicLeafRequestHandlerID,
			keyLength,
			pt,
		),
		targetRoot:   root,
		targetHeight: height,
		state:        state,
	}
}

// Sync fetches the atomic trie from a peer and applies it to the state,
// updating shared memory as it goes. Any error MUST be treated as fatal.
func (s *Syncer) Sync(ctx context.Context) error {
	// The fetcher only responds to requests for non-empty roots. And if we
	// already have the full state, no sense syncing again.
	if s.state.currentRoot != s.targetRoot {
		if err := s.sync(ctx); err != nil {
			return err
		}
	}

	// Update the shared memory markers to tip, since we have the most recent state
	// The recent blocks may not have had any atomic txs.
	if s.state.currentHeight.Load() < s.targetHeight {
		if err := s.state.writeToSharedMemory(s.state.db.NewBatch(), s.targetHeight, s.targetRoot, nil); err != nil {
			return fmt.Errorf("committing synced height %d: %w", s.targetHeight, err)
		}
	}
	return nil
}

func (s *Syncer) sync(ctx context.Context) error {
	w := newHeightWriter(s.state)

	for leaves, err := range collectLeaves(ctx,
		s.fetcher,
		s.targetRoot,
		firstKeyAfterHeight(s.state.currentHeight.Load()),
	) {
		if err != nil {
			return err
		}

		for i, key := range leaves.Keys {
			if len(key) != keyLength {
				return fmt.Errorf("unexpected trie key length %d, expected %d", len(key), keyLength)
			}

			// add MAY commit to [State].
			if err := w.add(key, leaves.Vals[i]); err != nil {
				return err
			}
		}
	}

	// Any remaining data also needs committed.
	if err := w.commit(); err != nil {
		return err
	}

	if s.state.currentRoot != s.targetRoot {
		return fmt.Errorf("synced root (%s) does not match target (%s) for cross-chain trie", s.state.currentRoot, s.targetRoot)
	}
	return nil
}

var errNoKeys = errors.New("no keys returned but more leaves expected")

// collectLeaves fetches the target trie's leaves from peers, starting with
// `start`. All returned [hashdb.Leaves] are in key order and have been proven
// to exist in the trie. Any error returned is fatal.
func collectLeaves(
	ctx context.Context,
	client *hashdb.Client,
	targetRoot common.Hash,
	start []byte,
) iter.Seq2[hashdb.Leaves, error] {
	const keyLimit = 1024
	return func(yield func(hashdb.Leaves, error) bool) {
		for {
			leaves, more, err := client.FetchLeaves(ctx, hashdb.LeafRange{
				Root:  targetRoot,
				Start: start,
				Limit: keyLimit,
			})
			if err != nil {
				yield(hashdb.Leaves{}, fmt.Errorf("fetching leaves: %w", err))
				return
			}

			// The consumer may retain and mutate the keys.
			var lastKey []byte
			if n := len(leaves.Keys); n > 0 {
				lastKey = common.CopyBytes(leaves.Keys[n-1])
			}

			if !yield(leaves, nil) || !more {
				return
			}
			if lastKey == nil {
				yield(hashdb.Leaves{}, errNoKeys)
				return
			}

			// Update start to be one bit past the last returned key for the next
			// request.
			start = lastKey
			hashdb.IncrementBytes(start)
		}
	}
}

// firstKeyAfterHeight returns the first trie key that would need synced, assuming all
// state up to currentHeight is already available.
func firstKeyAfterHeight(currentHeight uint64) []byte {
	return encodeTrieKey(currentHeight+1, ids.Empty)
}

// heightWriter accumulates the synced leaves of a single height, committing them
// to the state once the height is complete.
type heightWriter struct {
	state *State

	height uint64
	keys   [][]byte
	vals   [][]byte
	ops    map[ids.ID]*atomic.Requests
}

func newHeightWriter(state *State) *heightWriter {
	return &heightWriter{
		state: state,
		ops:   make(map[ids.ID]*atomic.Requests),
	}
}

// add accumulates a single synced leaf. Leaves arrive in key order, so a new
// height means all of the previous height's leaves have arrived and it can be
// committed before accumulating this one.
func (w *heightWriter) add(key, val []byte) error {
	height, chainID := decodeTrieKey(key)
	if height != w.height {
		if err := w.commit(); err != nil {
			return err
		}
		w.height = height
	}

	req := new(atomic.Requests)
	if _, err := c.Unmarshal(val, req); err != nil {
		return fmt.Errorf("unmarshaling atomic requests for chain %s: %w", chainID, err)
	}

	w.keys = append(w.keys, key)
	w.vals = append(w.vals, val)
	w.ops[chainID] = req
	return nil
}

// commit inserts all pending operations to the [triedb.Database] and to
// shared memory.
//
// This should be called whenever a new height is encountered and before
// finishing the sync.
func (w *heightWriter) commit() error {
	if len(w.keys) == 0 {
		return nil
	}

	newRoot, err := applyTrie(w.state.trieDB, w.state.currentRoot, w.keys, w.vals)
	if err != nil {
		return fmt.Errorf("applying synced trie at height %d: %w", w.height, err)
	}
	if err := w.state.writeToSharedMemory(w.state.db.NewBatch(), w.height, newRoot, w.ops); err != nil {
		return fmt.Errorf("committing synced height %d: %w", w.height, err)
	}

	w.keys = nil
	w.vals = nil
	w.ops = make(map[ids.ID]*atomic.Requests)
	return nil
}
