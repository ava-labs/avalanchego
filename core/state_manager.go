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

	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
)

const (
	commitInterval = 4096
	tipBufferSize  = 16
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
		return &cappedMemoryTrieWriter{
			TrieDB:             db,
			memoryCap:          common.StorageSize(config.TrieDirtyLimit) * 1024 * 1024,
			imageCap:           4 * 1024 * 1024,
			commitInterval:     commitInterval,
			tipBuffer:          make([]common.Hash, tipBufferSize),
			randomizedInterval: uint64(rand.Int63n(commitInterval)) + commitInterval,
		}
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
	return np.TrieDB.Commit(block.Root(), false, nil)
}

func (np *noPruningTrieWriter) AcceptTrie(block *types.Block) error { return nil }

func (np *noPruningTrieWriter) RejectTrie(block *types.Block) error { return nil }

func (np *noPruningTrieWriter) Shutdown() error { return nil }

type cappedMemoryTrieWriter struct {
	TrieDB
	memoryCap                          common.StorageSize
	imageCap                           common.StorageSize
	commitInterval, randomizedInterval uint64

	lastPos   int
	tipBuffer []common.Hash
}

func (cm *cappedMemoryTrieWriter) InsertTrie(block *types.Block) error {
	cm.TrieDB.Reference(block.Root(), common.Hash{})

	nodes, imgs := cm.TrieDB.Size()
	if nodes > cm.memoryCap || imgs > cm.imageCap {
		return cm.TrieDB.Cap(cm.memoryCap - ethdb.IdealBatchSize)
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
	nextPos := (cm.lastPos + 1) % tipBufferSize
	if cm.tipBuffer[nextPos] != (common.Hash{}) {
		cm.TrieDB.Dereference(cm.tipBuffer[nextPos])
	}
	cm.tipBuffer[nextPos] = root
	cm.lastPos = nextPos

	// Commit this root if we haven't committed an accepted block root within
	// the desired interval.
	// Note: a randomized interval is added here to ensure that pruning nodes
	// do not all only commit at the exact same heights.
	if height := block.NumberU64(); height%cm.commitInterval == 0 || height%cm.randomizedInterval == 0 {
		if err := cm.TrieDB.Commit(root, true, nil); err != nil {
			return fmt.Errorf("failed to commit trie for block %s: %w", block.Hash().Hex(), err)
		}
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
	if cm.tipBuffer[cm.lastPos] == (common.Hash{}) {
		return nil
	}

	// Attempt to commit last item added to [dereferenceQueue] on shutdown to avoid
	// re-processing the state on the next startup.
	return cm.TrieDB.Commit(cm.tipBuffer[cm.lastPos], true, nil)
}
