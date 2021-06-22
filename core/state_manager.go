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

	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
)

const (
	commitInterval = 4096
)

type TrieWriter interface {
	InsertTrie(block *types.Block) error // Insert reference to trie [root]
	AcceptTrie(block *types.Block) error // Mark [root] as part of an accepted block
	RejectTrie(block *types.Block) error // Notify TrieWriter that the block containing [root] has been rejected
	Shutdown() error
}

func NewTrieWriter(db state.Database, config *CacheConfig) TrieWriter {
	if config.Pruning {
		return &cappedMemoryTrieWriter{
			Database:           db,
			memoryCap:          common.StorageSize(config.TrieDirtyLimit) * 1024 * 1024,
			imageCap:           4 * 1024 * 1024,
			commitInterval:     commitInterval,
			randomizedInterval: uint64(rand.Int63n(commitInterval)) + commitInterval,
		}
	} else {
		return &noPruningTrieWriter{
			Database: db,
		}
	}
}

type noPruningTrieWriter struct {
	state.Database
}

func (np *noPruningTrieWriter) InsertTrie(block *types.Block) error {
	triedb := np.Database.TrieDB()
	return triedb.Commit(block.Root(), false, nil)
}

func (np *noPruningTrieWriter) AcceptTrie(block *types.Block) error { return nil }

func (np *noPruningTrieWriter) RejectTrie(block *types.Block) error { return nil }

func (np *noPruningTrieWriter) Shutdown() error { return nil }

type cappedMemoryTrieWriter struct {
	state.Database
	memoryCap                          common.StorageSize
	imageCap                           common.StorageSize
	lastAcceptedRoot                   common.Hash
	commitInterval, randomizedInterval uint64
}

func (cm *cappedMemoryTrieWriter) InsertTrie(block *types.Block) error {
	triedb := cm.Database.TrieDB()
	triedb.Reference(block.Root(), common.Hash{})

	nodes, imgs := triedb.Size()
	if nodes > cm.memoryCap || imgs > cm.imageCap {
		return triedb.Cap(cm.memoryCap - ethdb.IdealBatchSize)
	}

	return nil
}

func (cm *cappedMemoryTrieWriter) AcceptTrie(block *types.Block) error {
	triedb := cm.Database.TrieDB()
	root := block.Root()
	triedb.Dereference(cm.lastAcceptedRoot)
	cm.lastAcceptedRoot = root

	// Commit this root if we haven't committed an accepted block root within
	// the desired interval.
	// Note: a randomized interval is added here to ensure that pruning nodes
	// do not all only commit at the exact same heights.
	if height := block.NumberU64(); height%cm.commitInterval == 0 || height%cm.randomizedInterval == 0 {
		if err := triedb.Commit(root, true, nil); err != nil {
			return fmt.Errorf("failed to commit trie for block %s: %w", block.Hash().Hex(), err)
		}
	}
	return nil
}

func (cm *cappedMemoryTrieWriter) RejectTrie(block *types.Block) error {
	triedb := cm.Database.TrieDB()
	triedb.Dereference(block.Root())
	return nil
}

func (cm *cappedMemoryTrieWriter) Shutdown() error {
	// If [lastAcceptedRoot] is empty, no need to do any cleanup on
	// shutdown.
	if cm.lastAcceptedRoot == (common.Hash{}) {
		return nil
	}

	// Attempt to commit [lastAcceptedRoot] on shutdown to avoid
	// re-processing the state on the next startup.
	triedb := cm.Database.TrieDB()
	return triedb.Commit(cm.lastAcceptedRoot, true, nil)
}
