// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/message"
	synccommon "github.com/ava-labs/coreth/sync"
	syncclient "github.com/ava-labs/coreth/sync/client"

	"github.com/ava-labs/libevm/trie"
)

const (
	minNumWorkers     = 1
	maxNumWorkers     = 64
	defaultNumWorkers = 8 // TODO: Dynamic worker count discovery will be implemented in a future PR.

	minRequestSize = 1
	maxRequestSize = 1024 // Matches [maxLeavesLimit] in sync/handlers/leafs_request.go

	// TrieNode represents a leaf node that belongs to the atomic trie.
	TrieNode message.NodeType = 2
)

var (
	// errTooFewWorkers is returned when numWorkers is below the minimum.
	errTooFewWorkers = errors.New("numWorkers below minimum")

	// errTooManyWorkers is returned when numWorkers exceeds the maximum.
	errTooManyWorkers = errors.New("numWorkers above maximum")

	errNilClient           = errors.New("Client cannot be nil")
	errNilDatabase         = errors.New("Database cannot be nil")
	errNilAtomicTrie       = errors.New("AtomicTrie cannot be nil")
	errEmptyTargetRoot     = errors.New("TargetRoot cannot be empty")
	errInvalidTargetHeight = errors.New("TargetHeight must be greater than 0")
	errInvalidRequestSize  = errors.New("RequestSize must be between 1 and 1024")

	// Pre-allocate zero bytes to avoid repeated allocations.
	zeroBytes = bytes.Repeat([]byte{0x00}, common.HashLength)

	_ synccommon.Syncer       = (*syncer)(nil)
	_ syncclient.LeafSyncTask = (*syncerLeafTask)(nil)
)

// Config holds the configuration for creating a new atomic syncer.
type Config struct {
	// Client is the leaf client used to fetch data from the network.
	Client syncclient.LeafClient

	// Database is the version database for storing synced data.
	Database *versiondb.Database

	// AtomicTrie is the atomic trie to sync into.
	AtomicTrie *atomicstate.AtomicTrie

	// TargetRoot is the root hash of the trie to sync to.
	TargetRoot common.Hash

	// TargetHeight is the target block height to sync to.
	TargetHeight uint64

	// RequestSize is the maximum number of leaves to request in a single network call.
	RequestSize uint16

	// NumWorkers is the number of worker goroutines to use for syncing.
	// If not set, [DefaultNumWorkers] will be used.
	NumWorkers int
}

// Validate checks if the configuration is valid and returns an error if not.
func (c *Config) Validate() error {
	if c.Client == nil {
		return errNilClient
	}
	if c.Database == nil {
		return errNilDatabase
	}
	if c.AtomicTrie == nil {
		return errNilAtomicTrie
	}
	if c.TargetRoot == (common.Hash{}) {
		return errEmptyTargetRoot
	}
	if c.TargetHeight == 0 {
		return errInvalidTargetHeight
	}

	// Set default RequestSize if not specified.
	if c.RequestSize == 0 {
		c.RequestSize = config.DefaultStateSyncRequestSize
	}
	if c.RequestSize < minRequestSize || c.RequestSize > maxRequestSize {
		return fmt.Errorf("%w: %d (must be between %d and %d)", errInvalidRequestSize, c.RequestSize, minRequestSize, maxRequestSize)
	}

	// Validate NumWorkers.
	numWorkers := c.NumWorkers
	if numWorkers == 0 {
		numWorkers = defaultNumWorkers
	}
	if numWorkers < minNumWorkers {
		return fmt.Errorf("%w: %d (minimum: %d)", errTooFewWorkers, numWorkers, minNumWorkers)
	}
	if numWorkers > maxNumWorkers {
		return fmt.Errorf("%w: %d (maximum: %d)", errTooManyWorkers, numWorkers, maxNumWorkers)
	}

	return nil
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

	// numWorkers is the number of worker goroutines to use for syncing
	numWorkers int

	// cancel is used to cancel the sync operation
	cancel context.CancelFunc
}

// addZeros adds [common.HashLenth] zeros to [height] and returns the result as []byte
func addZeroes(height uint64) []byte {
	packer := wrappers.Packer{Bytes: make([]byte, atomicstate.TrieKeyLength)}
	packer.PackLong(height)
	packer.PackFixedBytes(zeroBytes)
	return packer.Bytes
}

// newSyncer returns a new syncer instance that will sync the atomic trie from the network.
func newSyncer(config *Config) (*syncer, error) {
	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Use default workers if not specified.
	numWorkers := config.NumWorkers
	if numWorkers == 0 {
		numWorkers = defaultNumWorkers
	}

	lastCommittedRoot, lastCommit := config.AtomicTrie.LastCommitted()
	trie, err := config.AtomicTrie.OpenTrie(lastCommittedRoot)
	if err != nil {
		return nil, err
	}

	syncer := &syncer{
		db:           config.Database,
		atomicTrie:   config.AtomicTrie,
		trie:         trie,
		targetRoot:   config.TargetRoot,
		targetHeight: config.TargetHeight,
		lastHeight:   lastCommit,
		numWorkers:   numWorkers,
	}

	// Create tasks channel with capacity for the number of workers.
	tasks := make(chan syncclient.LeafSyncTask, numWorkers)

	// For atomic trie syncing, we typically want a single task since the trie is sequential.
	// But we can create multiple tasks if needed for parallel processing of different ranges.
	tasks <- &syncerLeafTask{syncer: syncer}
	close(tasks)

	syncer.syncer = syncclient.NewCallbackLeafSyncer(config.Client, tasks, config.RequestSize)
	return syncer, nil
}

// Start begins syncing the target atomic root with the configured number of worker goroutines.
func (s *syncer) Start(ctx context.Context) error {
	if s.cancel != nil {
		return synccommon.ErrSyncerAlreadyStarted
	}

	ctx, s.cancel = context.WithCancel(ctx)
	s.syncer.Start(ctx, s.numWorkers, s.onSyncFailure)

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

// Wait blocks until the sync operation completes and returns any error that occurred.
// It respects context cancellation and returns ctx.Err() if the context is cancelled.
// This method must be called after Start() has been called.
func (s *syncer) Wait(ctx context.Context) error {
	if s.cancel == nil {
		return synccommon.ErrWaitBeforeStart
	}

	select {
	case err := <-s.syncer.Done():
		return err
	case <-ctx.Done():
		s.cancel()
		return ctx.Err()
	}
}

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
