// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ethereum/go-ethereum/common"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	// tipBufferSize is the number of recent accepted tries to keep in the TrieDB
	// dirties cache at tip (only applicable in [pruning] mode).
	//
	// Keeping extra tries around at tip enables clients to query data from
	// recent trie roots.
	tipBufferSize = 32

	// flushWindow is the distance to the [commitInterval] when we start
	// optimistically flushing trie nodes to disk (only applicable in [pruning]
	// mode).
	//
	// We perform this optimistic flushing to reduce synchronized database IO at the
	// [commitInterval].
	flushWindow = 768
)

type TrieWriter interface {
	InsertTrie(block *types.Block) error // Insert reference to trie [root]
	AcceptTrie(block *types.Block) error // Mark [root] as part of an accepted block
	RejectTrie(block *types.Block) error // Notify TrieWriter that the block containing [root] has been rejected
	Shutdown() error
}

type TrieDB interface {
	Reference(child common.Hash, parent common.Hash)
	Dereference(root common.Hash)
	Commit(root common.Hash, report bool, callback func(common.Hash)) error
	Size() (common.StorageSize, common.StorageSize)
	Cap(limit common.StorageSize) error
}

func NewTrieWriter(db TrieDB, config *CacheConfig) TrieWriter {
	if config.Pruning {
		cm := &cappedMemoryTrieWriter{
			TrieDB:           db,
			memoryCap:        common.StorageSize(config.TrieDirtyLimit) * 1024 * 1024,
			targetCommitSize: common.StorageSize(config.TrieDirtyCommitTarget) * 1024 * 1024,
			imageCap:         4 * 1024 * 1024,
			commitInterval:   config.CommitInterval,
			tipBuffer:        NewBoundedBuffer(tipBufferSize, db.Dereference),
		}
		cm.flushStepSize = (cm.memoryCap - cm.targetCommitSize) / common.StorageSize(flushWindow)
		return cm
	} else {
		return &noPruningTrieWriter{
			TrieDB: db,
		}
	}
}

type noPruningTrieWriter struct {
	TrieDB
}

func (np *noPruningTrieWriter) InsertTrie(block *types.Block) error {
	// We don't attempt to [Cap] here because we should never have
	// a significant amount of [TrieDB.Dirties] (we commit each block).
	np.TrieDB.Reference(block.Root(), common.Hash{})
	return nil
}

func (np *noPruningTrieWriter) AcceptTrie(block *types.Block) error {
	// We don't need to call [Dereference] on the block root at the end of this
	// function because it is removed from the [TrieDB.Dirties] map in [Commit].
	return np.TrieDB.Commit(block.Root(), false, nil)
}

func (np *noPruningTrieWriter) RejectTrie(block *types.Block) error {
	np.TrieDB.Dereference(block.Root())
	return nil
}

func (np *noPruningTrieWriter) Shutdown() error { return nil }

type cappedMemoryTrieWriter struct {
	TrieDB
	memoryCap        common.StorageSize
	targetCommitSize common.StorageSize
	flushStepSize    common.StorageSize
	imageCap         common.StorageSize
	commitInterval   uint64

	tipBuffer *BoundedBuffer
}

func (cm *cappedMemoryTrieWriter) InsertTrie(block *types.Block) error {
	cm.TrieDB.Reference(block.Root(), common.Hash{})

	// The use of [Cap] in [InsertTrie] prevents exceeding the configured memory
	// limit (and OOM) in case there is a large backlog of processing (unaccepted) blocks.
	nodes, imgs := cm.TrieDB.Size()
	if nodes <= cm.memoryCap && imgs <= cm.imageCap {
		return nil
	}
	if err := cm.TrieDB.Cap(cm.memoryCap - ethdb.IdealBatchSize); err != nil {
		return fmt.Errorf("failed to cap trie for block %s: %w", block.Hash().Hex(), err)
	}

	return nil
}

func (cm *cappedMemoryTrieWriter) AcceptTrie(block *types.Block) error {
	root := block.Root()

	// Attempt to dereference roots at least [tipBufferSize] old (so queries at tip
	// can still be completed).
	//
	// Note: It is safe to dereference roots that have been committed to disk
	// (they are no-ops).
	cm.tipBuffer.Insert(root)

	// Commit this root if we have reached the [commitInterval].
	modCommitInterval := block.NumberU64() % cm.commitInterval
	if modCommitInterval == 0 {
		if err := cm.TrieDB.Commit(root, true, nil); err != nil {
			return fmt.Errorf("failed to commit trie for block %s: %w", block.Hash().Hex(), err)
		}
		return nil
	}

	// Write at least [flushStepSize] of the oldest nodes in the trie database
	// dirty cache to disk as we approach the [commitInterval] to reduce the number of trie nodes
	// that will need to be written at once on [Commit] (to roughly [targetCommitSize]).
	//
	// To reduce the number of useless trie nodes that are committed during this
	// capping, we only optimistically flush within the [flushWindow]. During
	// this period, the [targetMemory] decreases stepwise by [flushStepSize]
	// as we get closer to the commit boundary.
	//
	// Most trie nodes are 300B, so we will write at least ~1000 trie nodes in
	// a single optimistic flush (with the default [flushStepSize]=312KB).
	distanceFromCommit := cm.commitInterval - modCommitInterval // this cannot be 0
	if distanceFromCommit > flushWindow {
		return nil
	}
	targetMemory := cm.targetCommitSize + cm.flushStepSize*common.StorageSize(distanceFromCommit)
	nodes, _ := cm.TrieDB.Size()
	if nodes <= targetMemory {
		return nil
	}
	targetCap := targetMemory - ethdb.IdealBatchSize
	if err := cm.TrieDB.Cap(targetCap); err != nil {
		return fmt.Errorf("failed to cap trie for block %s (target=%s): %w", block.Hash().Hex(), targetCap, err)
	}
	return nil
}

func (cm *cappedMemoryTrieWriter) RejectTrie(block *types.Block) error {
	cm.TrieDB.Dereference(block.Root())
	return nil
}

func (cm *cappedMemoryTrieWriter) Shutdown() error {
	// If [tipBuffer] entry is empty, no need to do any cleanup on
	// shutdown.
	last := cm.tipBuffer.Last()
	if last == (common.Hash{}) {
		return nil
	}

	// Attempt to commit last item added to [dereferenceQueue] on shutdown to avoid
	// re-processing the state on the next startup.
	return cm.TrieDB.Commit(last, true, nil)
}
