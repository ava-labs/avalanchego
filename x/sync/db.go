// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var (
	ErrInsufficientHistory = errors.New("insufficient history to generate proof")
	ErrNoEndRoot           = fmt.Errorf("%w: end root not found", ErrInsufficientHistory)
)

type Marshaler[T any] interface {
	Marshal(T) ([]byte, error)
	Unmarshal([]byte) (T, error)
}

type DB[TRange any, TChange any] interface {
	// GetMerkleRoot returns the current root of the trie.
	// If the trie is empty, returns ids.Empty.
	GetMerkleRoot(context.Context) (ids.ID, error)

	// Clear removes all key/value pairs from the trie.
	Clear() error

	// GetChangeProof returns a proof for a subset of the key/value changes in key range
	// [start, end] that occurred between [startRootID] and [endRootID].
	// Returns at most [maxLength] key/value pairs.
	// Returns [sync.ErrStartRootNotFound] if this node has insufficient
	// history to generate the proof.
	// Returns ErrEmptyProof if [endRootID] is ids.Empty.
	// Note that [endRootID] == ids.Empty means the trie is empty
	// (i.e. we don't need a change proof.)
	// Returns [sync.ErrEndRootNotFound], if the history doesn't contain the [endRootID].
	GetChangeProof(
		ctx context.Context,
		startRootID ids.ID,
		endRootID ids.ID,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		maxLength int,
	) (TChange, error)

	// Returns nil iff all the following hold:
	//   - [start] <= [end].
	//   - [proof] is non-empty.
	//   - [proof] is well-formed.
	//   - The keys in [proof] is not longer than [maxLength].
	//   - All keys in [proof] and [proof] are in [start, end].
	//     If [start] is nothing, all keys are considered > [start].
	//     If [end] is nothing, all keys are considered < [end].
	//   - When the changes in [proof] are applied,
	//     the root ID of the database is [expectedEndRootID].
	VerifyChangeProof(
		ctx context.Context,
		proof TChange,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		expectedEndRootID ids.ID,
		maxLength int,
	) error

	// CommitChangeProof commits the key/value pairs within the [proof] to the db.
	// [end] is the largest possible key in the range this [proof] covers.
	// Returns the next key that should be requested, or Nothing if all keys
	// have been are correct prior to [end].
	CommitChangeProof(ctx context.Context, end maybe.Maybe[[]byte], proof TChange) (maybe.Maybe[[]byte], error)

	// GetRangeProofAtRoot returns a proof for the key/value pairs in this trie within the range
	// [start, end] when the root of the trie was [rootID].
	// If [start] is Nothing, there's no lower bound on the range.
	// If [end] is Nothing, there's no upper bound on the range.
	// Returns an error if [rootID] is ids.Empty.
	// Note that [rootID] == ids.Empty means the trie is empty
	// (i.e. we don't need a range proof.)
	GetRangeProofAtRoot(
		ctx context.Context,
		rootID ids.ID,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		maxLength int,
	) (TRange, error)

	// Returns nil iff all the following hold:
	//   - [start] <= [end].
	//   - [proof] is non-empty.
	//   - [proof] is well-formed.
	//   - The keys in [proof] is not longer than [maxLength].
	//   - All keys in [proof] and [proof] are in [start, end].
	//     If [start] is nothing, all keys are considered > [start].
	//     If [end] is nothing, all keys are considered < [end].
	//   - When the changes in [proof] are applied,
	//     the root ID of the database is [expectedEndRootID].
	VerifyRangeProof(
		ctx context.Context,
		proof TRange,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		expectedEndRootID ids.ID,
		maxLength int,
	) error

	// CommitRangeProof commits the key/value pairs within the [proof] to the db.
	// [start] is the smallest possible key in the range this [proof] covers.
	// [end] is the largest possible key in the range this [proof] covers.
	// Returns the next key that should be requested, or Nothing if all keys
	// have been are correct prior to [end].
	CommitRangeProof(ctx context.Context, start, end maybe.Maybe[[]byte], proof TRange) (maybe.Maybe[[]byte], error)
}
