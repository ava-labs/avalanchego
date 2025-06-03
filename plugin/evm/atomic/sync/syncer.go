// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/libevm/common"

	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"

	"github.com/ava-labs/libevm/trie"
)

// TrieNode represents a leaf node that belongs to the atomic trie.
const TrieNode message.NodeType = 2

var (
	_ Syncer                  = (*syncer)(nil)
	_ syncclient.LeafSyncTask = (*syncerLeafTask)(nil)
)

// Syncer represents a step in state sync,
// along with Start/Done methods to control
// and monitor progress.
// Error returns an error if any was encountered.
type Syncer interface {
	Start(ctx context.Context) error
	Done() <-chan error
}

// syncer is used to sync the atomic trie from the network. The CallbackLeafSyncer
// is responsible for orchestrating the sync while syncer is responsible for maintaining
// the state of progress and writing the actual atomic trie to the trieDB.
type syncer struct {
	db           *versiondb.Database
	atomicTrie   *atomicstate.AtomicTrie
	trie         *trie.Trie // used to update the atomic trie
	targetRoot   common.Hash
	targetHeight uint64

	// syncer is used to sync leaves from the network.
	syncer *syncclient.CallbackLeafSyncer

	// lastHeight is the greatest height for which key / values
	// were last inserted into the [atomicTrie]
	lastHeight uint64
}

// addZeros adds [common.HashLenth] zeros to [height] and returns the result as []byte
func addZeroes(height uint64) []byte {
	packer := wrappers.Packer{Bytes: make([]byte, atomicstate.TrieKeyLength)}
	packer.PackLong(height)
	packer.PackFixedBytes(bytes.Repeat([]byte{0x00}, common.HashLength))
	return packer.Bytes
}

func newSyncer(client syncclient.LeafClient, vdb *versiondb.Database, atomicTrie *atomicstate.AtomicTrie, targetRoot common.Hash, targetHeight uint64, requestSize uint16) (*syncer, error) {
	lastCommittedRoot, lastCommit := atomicTrie.LastCommitted()
	trie, err := atomicTrie.OpenTrie(lastCommittedRoot)
	if err != nil {
		return nil, err
	}

	syncer := &syncer{
		db:           vdb,
		atomicTrie:   atomicTrie,
		trie:         trie,
		targetRoot:   targetRoot,
		targetHeight: targetHeight,
		lastHeight:   lastCommit,
	}
	tasks := make(chan syncclient.LeafSyncTask, 1)
	tasks <- &syncerLeafTask{syncer: syncer}
	close(tasks)
	syncer.syncer = syncclient.NewCallbackLeafSyncer(client, tasks, requestSize)
	return syncer, nil
}

// Start begins syncing the target atomic root.
func (s *syncer) Start(ctx context.Context) error {
	s.syncer.Start(ctx, 1, s.onSyncFailure)
	return nil
}

// onLeafs is the callback for the leaf syncer, which will insert the key-value pairs into the trie.
func (s *syncer) onLeafs(keys [][]byte, values [][]byte) error {
	for i, key := range keys {
		if len(key) != atomicstate.TrieKeyLength {
			return fmt.Errorf("unexpected key len (%d) in atomic trie sync", len(key))
		}
		// key = height + blockchainID
		height := binary.BigEndian.Uint64(key[:wrappers.LongLen])
		if height > s.lastHeight {
			// If this key belongs to a new height, we commit
			// the trie at the previous height before adding this key.
			root, nodes, err := s.trie.Commit(false)
			if err != nil {
				return err
			}
			if err := s.atomicTrie.InsertTrie(nodes, root); err != nil {
				return err
			}
			// AcceptTrie commits the trieDB and returns [isCommit] as true
			// if we have reached or crossed a commit interval.
			isCommit, err := s.atomicTrie.AcceptTrie(s.lastHeight, root)
			if err != nil {
				return err
			}
			if isCommit {
				// Flush pending changes to disk to preserve progress and
				// free up memory if the trieDB was committed.
				if err := s.db.Commit(); err != nil {
					return err
				}
			}
			// Trie must be re-opened after committing (not safe for re-use after commit)
			trie, err := s.atomicTrie.OpenTrie(root)
			if err != nil {
				return err
			}
			s.trie = trie
			s.lastHeight = height
		}

		if err := s.trie.Update(key, values[i]); err != nil {
			return err
		}
	}
	return nil
}

// onFinish is called when sync for this trie is complete.
// commit the trie to disk and perform the final checks that we synced the target root correctly.
func (s *syncer) onFinish() error {
	// commit the trie on finish
	root, nodes, err := s.trie.Commit(false)
	if err != nil {
		return err
	}
	if err := s.atomicTrie.InsertTrie(nodes, root); err != nil {
		return err
	}
	if _, err := s.atomicTrie.AcceptTrie(s.targetHeight, root); err != nil {
		return err
	}
	if err := s.db.Commit(); err != nil {
		return err
	}

	// the root of the trie should always match the targetRoot  since we already verified the proofs,
	// here we check the root mainly for correctness of the atomicTrie's pointers and it should never fail.
	if s.targetRoot != root {
		return fmt.Errorf("synced root (%s) does not match expected (%s) for atomic trie ", root, s.targetRoot)
	}
	return nil
}

// onSyncFailure is a no-op since we flush progress to disk at the regular commit interval when syncing
// the atomic trie.
func (s *syncer) onSyncFailure(error) error {
	return nil
}

// Done returns a channel which produces any error that occurred during syncing or nil on success.
func (s *syncer) Done() <-chan error { return s.syncer.Done() }

type syncerLeafTask struct {
	syncer *syncer
}

func (a *syncerLeafTask) Start() []byte                  { return addZeroes(a.syncer.lastHeight + 1) }
func (a *syncerLeafTask) End() []byte                    { return nil }
func (a *syncerLeafTask) NodeType() message.NodeType     { return TrieNode }
func (a *syncerLeafTask) OnFinish(context.Context) error { return a.syncer.onFinish() }
func (a *syncerLeafTask) OnStart() (bool, error)         { return false, nil }
func (a *syncerLeafTask) Root() common.Hash              { return a.syncer.targetRoot }
func (a *syncerLeafTask) Account() common.Hash           { return common.Hash{} }
func (a *syncerLeafTask) OnLeafs(keys, vals [][]byte) error {
	return a.syncer.onLeafs(keys, vals)
}
