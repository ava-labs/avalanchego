// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saedb provides functionality related to storage and access of
// [Streaming Asynchronous Execution] data.
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package saedb

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
)

// ErrStateUnavailable means the requested trie state is not held by the
// configured backend. Callers MAY recover it from an earlier state.
var ErrStateUnavailable = errors.New("state unavailable")

// ShouldCommitTrieDB returns whether or not to commit the state trie to disk.
func ShouldCommitTrieDB(blockNum, commitInterval uint64) bool {
	return blockNum%commitInterval == 0
}

// LastCommittedTrieDBHeight returns the largest value <= the argument at which
// [ShouldCommitTrieDB] would have returned true.
func LastCommittedTrieDBHeight(atOrBefore, commitInterval uint64) uint64 {
	return atOrBefore - atOrBefore%commitInterval
}

// A StateDBOpener opens a [state.StateDB] at the given root.
type StateDBOpener interface {
	StateDB(root common.Hash) (*state.StateDB, error)
}
