// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saedb provides functionality related to storage and access of
// [Streaming Asynchronous Execution] data.
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package saedb

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
)

const (
	// CommitTrieDBEvery is the number of blocks between commits of the state
	// trie to disk.
	CommitTrieDBEvery     = 1 << commitTrieDBEveryLog2
	commitTrieDBEveryLog2 = 12
	commitTrieDBMask      = CommitTrieDBEvery - 1
)

// ShouldCommitTrieDB returns whether or not to commit the state trie to disk.
func ShouldCommitTrieDB(blockNum uint64) bool {
	return blockNum&commitTrieDBMask == 0
}

// LastCommittedTrieDBHeight returns the largest value <= the argument at which
// [ShouldCommitTrieDB] would have returned true.
func LastCommittedTrieDBHeight(atOrBefore uint64) uint64 {
	return atOrBefore &^ commitTrieDBMask
}

// A StateDBOpener opens a [state.StateDB] at the given root.
type StateDBOpener interface {
	StateDB(root common.Hash) (*state.StateDB, error)
}

// ExecutionResults provides type safety for a [database.HeightIndex], to be
// used for persistence of SAE-specific execution results, avoiding possible
// collision with `rawdb` keys.
type ExecutionResults struct {
	database.HeightIndex
}
