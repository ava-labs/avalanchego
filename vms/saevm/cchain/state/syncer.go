// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
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

// Syncer fetches the cross-chain state from peers, applies it to a [State],
// and updates shared memory as it goes.
type Syncer struct {
	fetcher *hashdb.Client

	state        *State
	targetRoot   common.Hash
	targetHeight uint64
}

// NewSyncer creates a new cross-chain trie syncer.
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

// Sync fetches cross-chain state from a peer and applies it to the [State],
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
	// The recent blocks MAY have had no cchain txs.
	if s.state.currentHeight.Load() < s.targetHeight {
		if err := s.state.writeToSharedMemory(s.state.db.NewBatch(), s.targetHeight, s.targetRoot, nil); err != nil {
			return fmt.Errorf("committing synced height %d: %w", s.targetHeight, err)
		}
	}
	return nil
}

func (s *Syncer) sync(ctx context.Context) error {
	for batch, err := range collectLeaves(ctx,
		s.fetcher,
		s.targetRoot,
		firstKeyAfterHeight(s.state.currentHeight.Load()),
	) {
		if err != nil {
			return err
		}
		if err := s.commit(batch); err != nil {
			return err
		}
	}

	if s.state.currentRoot != s.targetRoot {
		return fmt.Errorf("synced root (%s) does not match target (%s) for cross-chain trie", s.state.currentRoot, s.targetRoot)
	}
	return nil
}

// firstKeyAfterHeight returns the first trie key that would need synced, assuming all
// state up to currentHeight is already available.
func firstKeyAfterHeight(currentHeight uint64) []byte {
	return encodeTrieKey(currentHeight+1, ids.Empty)
}

// collectLeaves fetches the target trie's leaves from peers, starting with
// `start`. All returned [heightBatch]s are in key order and have been proven to
// exist in the trie. Only one [heightBatch] will be returned for any given
// height. There MAY be skipped heights. Any error returned is fatal.
func collectLeaves(
	ctx context.Context,
	client *hashdb.Client,
	targetRoot common.Hash,
	start []byte,
) iter.Seq2[heightBatch, error] {
	const keyLimit = 1024
	return func(yield func(heightBatch, error) bool) {
		batch := heightBatch{height: decodeTrieKeyHeight(start)}
		for {
			leaves, more, err := client.FetchLeaves(ctx, hashdb.LeafRange{
				Root:  targetRoot,
				Start: start,
				Limit: keyLimit,
			})
			if err != nil {
				yield(heightBatch{}, fmt.Errorf("fetching leaves: %w", err))
				return
			}

			for i, key := range leaves.Keys {
				if h := decodeTrieKeyHeight(key); h != batch.height {
					if !yield(batch, nil) {
						return
					}
					batch = heightBatch{height: h}
				}

				batch.add(key, leaves.Vals[i])
			}

			if !more {
				yield(batch, nil)
				return
			}

			// The [hashdb.Client] guarantees to return a non-empty set of keys
			// when `more` is true.
			start = hashdb.NextKey(leaves.Keys[len(leaves.Keys)-1])
		}
	}
}

// heightBatch accumulates the synced leaves of a single height.
type heightBatch struct {
	height uint64
	keys   [][]byte
	vals   [][]byte
}

// add accumulates a single synced leaf.
func (w *heightBatch) add(key, val []byte) {
	w.keys = append(w.keys, key)
	w.vals = append(w.vals, val)
}

// commit inserts keys and values for a height to the [triedb.Database] and to
// shared memory.
func (s *Syncer) commit(b heightBatch) error {
	if len(b.keys) == 0 {
		return nil
	}

	newRoot, err := applyTrie(s.state.trieDB, s.state.currentRoot, b.keys, b.vals)
	if err != nil {
		return fmt.Errorf("applying synced trie at height %d: %w", b.height, err)
	}

	ops := make(map[ids.ID]*atomic.Requests)
	for i, key := range b.keys {
		req := new(atomic.Requests)
		if _, err := c.Unmarshal(b.vals[i], req); err != nil {
			return fmt.Errorf("unmarshaling atomic requests: %w", err)
		}
		ops[decodeTrieKeyChainID(key)] = req
	}
	if err := s.state.writeToSharedMemory(s.state.db.NewBatch(), b.height, newRoot, ops); err != nil {
		return fmt.Errorf("committing synced height %d: %w", b.height, err)
	}

	return nil
}
