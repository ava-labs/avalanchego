// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"fmt"
	"iter"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"
)

// RegisterSyncHandler allows the [State] to serve its data to state-syncing
// peers, counting the served requests on reg.
func RegisterSyncHandler(n *p2p.Network, state *State, reg prometheus.Registerer) error {
	return hashdb.RegisterHandler(state.snowCtx.Log, n, p2p.EVMAtomicLeafRequestHandlerID, state.trieDB, keyLength, reg)
}

// Syncer fetches the cross-chain state from peers, applies it to a [State],
// and updates shared memory as it goes. If syncing finishes without error, it
// is guaranteed that a full cross-chain state for the target root and height
// is available in the [State].  The [State] MAY NOT have all intermediate
// states.
type Syncer struct {
	fetcher *hashdb.Client

	state        *State
	targetRoot   common.Hash
	targetHeight uint64
}

// NewSyncer creates a new cross-chain trie syncer, counting its requests on m.
// The [State] MUST NOT be altered concurrently with the syncer.
func NewSyncer(n *p2p.Network, pt *p2p.PeerTracker, state *State, root common.Hash, height uint64, m *network.Metrics) *Syncer {
	return &Syncer{
		fetcher: hashdb.NewClient(
			state.snowCtx.Log,
			n,
			p2p.EVMAtomicLeafRequestHandlerID,
			keyLength,
			pt,
			m,
		),
		targetRoot:   root,
		targetHeight: height,
		state:        state,
	}
}

// Sync fetches cross-chain state from a peer and applies it to the [State],
// updating shared memory as it goes. Any error MUST be treated as fatal.
func (s *Syncer) Sync(ctx context.Context) error {
	if s.state.currentHeight.Load() >= s.targetHeight {
		return nil
	}

	// The fetcher only responds to requests for non-empty roots.
	if types.EmptyRootHash != s.targetRoot {
		if err := s.sync(ctx); err != nil {
			return err
		}
	}

	// Update the shared memory markers to tip, since we have the most recent state
	// The recent blocks MAY have had no cchain txs.
	if s.state.currentHeight.Load() < s.targetHeight {
		if err := s.state.commit(s.state.db.NewBatch(), s.targetHeight, nil); err != nil {
			return fmt.Errorf("committing synced height %d: %w", s.targetHeight, err)
		}
	}
	return nil
}

func (s *Syncer) sync(ctx context.Context) error {
	blocks := iterateHeights(
		ctx,
		s.fetcher,
		s.targetRoot,
		s.state.currentHeight.Load(),
	)
	for block, err := range blocks {
		if err != nil {
			return err
		}
		if err := s.state.commit(s.state.db.NewBatch(), block.height, block.ops); err != nil {
			return err
		}
	}

	if s.state.currentRoot != s.targetRoot {
		return fmt.Errorf("synced root (%s) does not match target (%s) for cross-chain trie", s.state.currentRoot, s.targetRoot)
	}
	return nil
}

// heightBatch is the complete set of one height's cross-chain requests.
type heightBatch struct {
	height uint64
	ops    map[ids.ID]*atomic.Requests
}

// iterateHeights yields the operations at each height of the cross-chain trie
// rooted at root, for heights strictly above afterHeight, in ascending height
// order.
//
// A height is yielded only once all of its leaves have arrived, so its ops are
// never partial. A [heightBatch] is always non-empty.
func iterateHeights(
	ctx context.Context,
	client *hashdb.Client,
	root common.Hash,
	afterHeight uint64,
) iter.Seq2[heightBatch, error] {
	return func(yield func(heightBatch, error) bool) {
		current := heightBatch{
			ops: make(map[ids.ID]*atomic.Requests),
		}
		for l, err := range iterateLeaves(ctx, client, root, afterHeight) {
			if err != nil {
				yield(heightBatch{}, err)
				return
			}

			if l.height != current.height && len(current.ops) > 0 {
				if !yield(current, nil) {
					return
				}
				current.ops = make(map[ids.ID]*atomic.Requests)
			}

			current.height = l.height
			current.ops[l.chainID] = l.requests
		}
		if len(current.ops) > 0 {
			yield(current, nil)
		}
	}
}

// leaf is one decoded entry of the cross-chain trie.
type leaf struct {
	height   uint64
	chainID  ids.ID
	requests *atomic.Requests
}

// iterateLeaves iterates through each leaf of the trie rooted at root with
// heights strictly above afterHeight, decoded, in ascending key order.
func iterateLeaves(
	ctx context.Context,
	client *hashdb.Client,
	root common.Hash,
	afterHeight uint64,
) iter.Seq2[leaf, error] {
	return func(yield func(leaf, error) bool) {
		start := encodeTrieKey(afterHeight+1, ids.Empty)
		for {
			leaves, more, err := client.FetchLeaves(ctx, hashdb.LeafRange{
				Root:  root,
				Start: start,
				Limit: 1024,
			})
			if err != nil {
				yield(leaf{}, fmt.Errorf("fetching leaves: %w", err))
				return
			}

			for i, key := range leaves.Keys {
				if !yield(decodeLeaf(key, leaves.Vals[i])) {
					return
				}
			}
			if !more {
				return
			}

			// The [hashdb.Client] guarantees to return a non-empty set of keys
			// when `more` is true.
			last := leaves.Keys[len(leaves.Keys)-1]
			start = hashdb.NextKey(last)
		}
	}
}

// decodeLeaf splits a cross-chain trie entry into its height, chainID, and
// requests.
func decodeLeaf(key, val []byte) (leaf, error) {
	height, chainID, err := decodeTrieKey(key)
	if err != nil {
		return leaf{}, err
	}

	requests := new(atomic.Requests)
	if _, err := c.Unmarshal(val, requests); err != nil {
		return leaf{}, fmt.Errorf("unmarshaling atomic requests for chain %s: %w", chainID, err)
	}
	return leaf{
		height:   height,
		chainID:  chainID,
		requests: requests,
	}, nil
}
