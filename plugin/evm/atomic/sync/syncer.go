// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/coreth/plugin/evm/message"

	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	synccommon "github.com/ava-labs/coreth/sync"
	syncclient "github.com/ava-labs/coreth/sync/client"
)

const (
	defaultNumWorkers  = 8 // TODO: Dynamic worker count discovery will be implemented in a future PR.
	defaultRequestSize = 1024

	// TrieNode represents a leaf node that belongs to the atomic trie.
	TrieNode message.NodeType = 2
)

var (
	errInvalidTargetHeight = errors.New("TargetHeight must be greater than 0")

	// Pre-allocate zero bytes to avoid repeated allocations.
	zeroBytes = bytes.Repeat([]byte{0x00}, common.HashLength)

	_ synccommon.Syncer       = (*syncer)(nil)
	_ syncclient.LeafSyncTask = (*syncerLeafTask)(nil)
)

// Name returns the human-readable name for this sync task.
func (*syncer) Name() string { return "Atomic State Syncer" }

// ID returns the stable identifier for this sync task.
func (*syncer) ID() string { return "state_atomic_sync" }

// Config holds the configuration for creating a new atomic syncer.
type Config struct {
	// TargetHeight is the target block height to sync to.
	TargetHeight uint64

	// RequestSize is the maximum number of leaves to request in a single network call.
	// NOTE: user facing option validated as the parameter [plugin/evm/config.Config.StateSyncRequestSize].
	RequestSize uint16

	// NumWorkers is the number of worker goroutines to use for syncing.
	// If not set, [defaultNumWorkers] will be used.
	NumWorkers int
}

// WithUnsetDefaults returns a copy of the config with defaults applied for any
// unset (zero) fields.
func (c Config) WithUnsetDefaults() Config {
	out := c
	if out.NumWorkers == 0 {
		out.NumWorkers = defaultNumWorkers
	}
	if out.RequestSize == 0 {
		out.RequestSize = defaultRequestSize
	}

	return out
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
	packer.PackFixedBytes(zeroBytes)
	return packer.Bytes
}

// newSyncer returns a new syncer instance that will sync the atomic trie from the network.
func newSyncer(client syncclient.LeafClient, db *versiondb.Database, atomicTrie *atomicstate.AtomicTrie, targetRoot common.Hash, config *Config) (*syncer, error) {
	if config.TargetHeight == 0 {
		return nil, errInvalidTargetHeight
	}

	// Apply defaults for unset fields.
	cfg := config.WithUnsetDefaults()

	lastCommittedRoot, lastCommit := atomicTrie.LastCommitted()
	trie, err := atomicTrie.OpenTrie(lastCommittedRoot)
	if err != nil {
		return nil, err
	}

	syncer := &syncer{
		db:           db,
		atomicTrie:   atomicTrie,
		trie:         trie,
		targetRoot:   targetRoot,
		targetHeight: cfg.TargetHeight,
		lastHeight:   lastCommit,
	}

	// Create tasks channel with capacity for the number of workers.
	tasks := make(chan syncclient.LeafSyncTask, cfg.NumWorkers)

	// For atomic trie syncing, we typically want a single task since the trie is sequential.
	// But we can create multiple tasks if needed for parallel processing of different ranges.
	tasks <- &syncerLeafTask{syncer: syncer}
	close(tasks)

	syncer.syncer = syncclient.NewCallbackLeafSyncer(client, tasks, &syncclient.LeafSyncerConfig{
		RequestSize: cfg.RequestSize,
		NumWorkers:  cfg.NumWorkers,
		OnFailure:   func() {}, // No-op since we flush progress to disk at the regular commit interval.
	})

	return syncer, nil
}

// Sync begins syncing the target atomic root with the configured number of worker goroutines.
func (s *syncer) Sync(ctx context.Context) error {
	return s.syncer.Sync(ctx)
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

type syncerLeafTask struct {
	syncer *syncer
}

func (a *syncerLeafTask) Start() []byte                  { return addZeroes(a.syncer.lastHeight + 1) }
func (*syncerLeafTask) End() []byte                      { return nil }
func (*syncerLeafTask) NodeType() message.NodeType       { return TrieNode }
func (a *syncerLeafTask) OnFinish(context.Context) error { return a.syncer.onFinish() }
func (*syncerLeafTask) OnStart() (bool, error)           { return false, nil }
func (a *syncerLeafTask) Root() common.Hash              { return a.syncer.targetRoot }
func (*syncerLeafTask) Account() common.Hash             { return common.Hash{} }
func (a *syncerLeafTask) OnLeafs(keys, vals [][]byte) error {
	return a.syncer.onLeafs(keys, vals)
}
