// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	atomicstate "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	syncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
)

const (
	defaultNumWorkers  = 8 // TODO: Dynamic worker count discovery will be implemented in a future PR.
	defaultRequestSize = 1024

	// TrieNode represents a leaf node that belongs to the atomic trie.
	TrieNode message.NodeType = 2
)

var (
	_ sync.Syncer             = (*Syncer)(nil)
	_ syncclient.LeafSyncTask = (*syncerLeafTask)(nil)
	_ sync.Finalizer          = (*Syncer)(nil)

	errTargetHeightRequired = errors.New("target height must be > 0")
)

// config holds the configuration for creating a new atomic syncer.
type config struct {
	// requestSize is the maximum number of leaves to request in a single network call.
	// NOTE: user facing option validated as the parameter [plugin/evm/config.Config.StateSyncRequestSize].
	requestSize uint16

	// numWorkers is the number of worker goroutines to use for syncing.
	// If not set, [defaultNumWorkers] will be used.
	numWorkers int
}

// SyncerOption configures the atomic syncer via functional options.
type SyncerOption = options.Option[config]

// WithRequestSize sets the request size per network call.
func WithRequestSize(n uint16) SyncerOption {
	return options.Func[config](func(c *config) {
		if n > 0 {
			c.requestSize = n
		}
	})
}

// WithNumWorkers sets the number of worker goroutines for syncing.
func WithNumWorkers(n int) SyncerOption {
	return options.Func[config](func(c *config) {
		if n > 0 {
			c.numWorkers = n
		}
	})
}

// Syncer is used to sync the atomic trie from the network. The CallbackLeafSyncer
// is responsible for orchestrating the sync while Syncer is responsible for maintaining
// the state of progress and writing the actual atomic trie to the trieDB.
type Syncer struct {
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

	// Track sync progress for diagnostics
	totalKeysReceived   int    // Total number of keys processed
	lastCommittedRoot   common.Hash // Last committed root for validation
	commitCount         int    // Number of commits performed
}

// NewSyncer returns a new syncer instance that will sync the atomic trie from the network.
func NewSyncer(client syncclient.LeafClient, db *versiondb.Database, atomicTrie *atomicstate.AtomicTrie, targetRoot common.Hash, targetHeight uint64, opts ...SyncerOption) (*Syncer, error) {
	if targetHeight == 0 {
		return nil, errTargetHeightRequired
	}

	cfg := config{
		numWorkers:  defaultNumWorkers,
		requestSize: defaultRequestSize,
	}
	options.ApplyTo(&cfg, opts...)

	lastCommittedRoot, lastCommit := atomicTrie.LastCommitted()
	trie, err := atomicTrie.OpenTrie(lastCommittedRoot)
	if err != nil {
		return nil, err
	}

	syncer := &Syncer{
		db:           db,
		atomicTrie:   atomicTrie,
		trie:         trie,
		targetRoot:   targetRoot,
		targetHeight: targetHeight,
		lastHeight:   lastCommit,
	}

	// Create tasks channel with capacity for the number of workers.
	tasks := make(chan syncclient.LeafSyncTask, cfg.numWorkers)

	// For atomic trie syncing, we typically want a single task since the trie is sequential.
	// But we can create multiple tasks if needed for parallel processing of different ranges.
	tasks <- &syncerLeafTask{syncer: syncer}
	close(tasks)

	syncer.syncer = syncclient.NewCallbackLeafSyncer(client, tasks, &syncclient.LeafSyncerConfig{
		RequestSize: cfg.requestSize,
		NumWorkers:  cfg.numWorkers,
	})

	return syncer, nil
}

// Name returns the human-readable name for this sync task.
func (*Syncer) Name() string {
	return "Atomic State Syncer"
}

// ID returns the stable identifier for this sync task.
func (*Syncer) ID() string {
	return "state_atomic_sync"
}

// Sync begins syncing the target atomic root with the configured number of worker goroutines.
func (s *Syncer) Sync(ctx context.Context) error {
	return s.syncer.Sync(ctx)
}

// Finalize commits any pending database changes to disk.
// This ensures that even if the sync is cancelled or fails, we preserve
// the progress up to the last fully synced height.
func (s *Syncer) Finalize() error {
	return s.db.Commit()
}

// addZeroes returns the big-endian representation of `height`, prefixed with [common.HashLength] zeroes.
func addZeroes(height uint64) []byte {
	// Key format is [height(8 bytes)][blockchainID(32 bytes)]. Start should be the
	// smallest key for the given height, i.e., height followed by zeroed blockchainID.
	b := make([]byte, wrappers.LongLen+common.HashLength)
	binary.BigEndian.PutUint64(b[:wrappers.LongLen], height)
	return b
}

// onLeafs is the callback for the leaf syncer, which will insert the key-value pairs into the trie.
func (s *Syncer) onLeafs(ctx context.Context, keys [][]byte, values [][]byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.totalKeysReceived += len(keys)

	for i, key := range keys {
		if len(key) != atomicstate.TrieKeyLength {
			return fmt.Errorf("unexpected key len (%d) in atomic trie sync at key %d (expected %d)",
				len(key), i, atomicstate.TrieKeyLength)
		}
		// key = height + blockchainID
		height := binary.BigEndian.Uint64(key[:wrappers.LongLen])
		if height > s.lastHeight {
			// If this key belongs to a new height, we commit
			// the trie at the previous height before adding this key.
			root, nodes, err := s.trie.Commit(false)
			if err != nil {
				return fmt.Errorf("failed to commit trie at height %d: %w", s.lastHeight, err)
			}
			if err := s.atomicTrie.InsertTrie(nodes, root); err != nil {
				return fmt.Errorf("failed to insert trie nodes at height %d: %w", s.lastHeight, err)
			}
			// AcceptTrie commits the trieDB and returns [isCommit] as true
			// if we have reached or crossed a commit interval.
			isCommit, err := s.atomicTrie.AcceptTrie(s.lastHeight, root)
			if err != nil {
				return fmt.Errorf("failed to accept trie at height %d: %w", s.lastHeight, err)
			}

			s.commitCount++
			s.lastCommittedRoot = root

			if isCommit {
				// Flush pending changes to disk to preserve progress and
				// free up memory if the trieDB was committed.
				if err := s.db.Commit(); err != nil {
					return fmt.Errorf("failed to commit database at height %d: %w", s.lastHeight, err)
				}

				// Log progress every database commit for visibility
				log.Info("Atomic trie sync progress",
					"lastHeight", s.lastHeight,
					"newHeight", height,
					"committedRoot", root.Hex()[:16]+"...",
					"targetRoot", s.targetRoot.Hex()[:16]+"...",
					"totalKeys", s.totalKeysReceived,
					"commits", s.commitCount)
			}
			// Trie must be re-opened after committing (not safe for re-use after commit)
			trie, err := s.atomicTrie.OpenTrie(root)
			if err != nil {
				return fmt.Errorf("failed to reopen trie after commit at height %d: %w", s.lastHeight, err)
			}
			s.trie = trie
			s.lastHeight = height
		}

		if err := s.trie.Update(key, values[i]); err != nil {
			return fmt.Errorf("failed to update trie with key at index %d: %w", i, err)
		}
	}
	return nil
}

// onFinish is called when sync for this trie is complete.
// commit the trie to disk and perform the final checks that we synced the target root correctly.
func (s *Syncer) onFinish() error {
	// commit the trie on finish
	root, nodes, err := s.trie.Commit(false)
	if err != nil {
		return fmt.Errorf("failed to commit atomic trie: %w", err)
	}
	if err := s.atomicTrie.InsertTrie(nodes, root); err != nil {
		return fmt.Errorf("failed to insert trie nodes: %w", err)
	}
	if _, err := s.atomicTrie.AcceptTrie(s.targetHeight, root); err != nil {
		return fmt.Errorf("failed to accept trie at height %d: %w", s.targetHeight, err)
	}
	if err := s.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit database: %w", err)
	}

	// CRITICAL: Verify the synced root matches the target root.
	// This validates that all data received from peers was correct and consistent.
	// If this fails, it indicates:
	// - Peers provided inconsistent data across multiple requests
	// - Data corruption during network transmission
	// - Database corruption during incremental commits
	// - Race condition in atomic state updates
	if s.targetRoot != root {
		log.Error("FATAL: Atomic state sync root mismatch detected!",
			"syncedRoot", root.Hex(),
			"expectedRoot", s.targetRoot.Hex(),
			"targetHeight", s.targetHeight,
			"lastHeight", s.lastHeight,
			"totalKeys", s.totalKeysReceived,
			"commits", s.commitCount,
			"lastCommittedRoot", s.lastCommittedRoot.Hex())

		return fmt.Errorf(
			"FATAL ERROR! C-Chain atomic state sync failed with root mismatch:\n"+
				"  - Synced root: %s\n"+
				"  - Expected root: %s\n"+
				"  - Target height: %d\n"+
				"  - Last height processed: %d\n"+
				"  - Total keys received: %d\n"+
				"  - Commits performed: %d\n"+
				"  - Last committed root: %s\n"+
				"  - This indicates data corruption or peer inconsistency.\n"+
				"  - RECOMMENDED ACTION: Clear atomic state sync progress and retry from scratch.\n"+
				"  - If this persists, try different bootstrap peers.",
			root.Hex(), s.targetRoot.Hex(), s.targetHeight, s.lastHeight,
			s.totalKeysReceived, s.commitCount, s.lastCommittedRoot.Hex())
	}

	log.Info("Atomic state sync completed successfully!",
		"targetHeight", s.targetHeight,
		"root", root.Hex(),
		"totalKeys", s.totalKeysReceived,
		"commits", s.commitCount)
	return nil
}

type syncerLeafTask struct {
	syncer *Syncer
}

func (a *syncerLeafTask) Start() []byte                  { return addZeroes(a.syncer.lastHeight + 1) }
func (*syncerLeafTask) End() []byte                      { return nil }
func (*syncerLeafTask) NodeType() message.NodeType       { return TrieNode }
func (a *syncerLeafTask) OnFinish(context.Context) error { return a.syncer.onFinish() }
func (*syncerLeafTask) OnStart() (bool, error)           { return false, nil }
func (a *syncerLeafTask) Root() common.Hash              { return a.syncer.targetRoot }
func (*syncerLeafTask) Account() common.Hash             { return common.Hash{} }
func (a *syncerLeafTask) OnLeafs(ctx context.Context, keys, vals [][]byte) error {
	return a.syncer.onLeafs(ctx, keys, vals)
}
