// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

var _ DB[merkledb.RangeProof, merkledb.ChangeProof] = (merkledb.MerkleDB)(nil)

type DB[T, U any] interface {
	GetMerkleRoot(context.Context) (ids.ID, error)
	Clear() error
	RangeProofer[T]
	ChangeProofer[U]
}
type ChangeProofer[T any] interface {
	// GetChangeProof returns a proof for a subset of the key/value changes in key range
	// [start, end] that occurred between [startRootID] and [endRootID].
	// Returns at most [maxLength] key/value pairs.
	// Returns [ErrInsufficientHistory] if this node has insufficient history
	// to generate the proof.
	// Returns ErrEmptyProof if [endRootID] is ids.Empty.
	// Note that [endRootID] == ids.Empty means the trie is empty
	// (i.e. we don't need a change proof.)
	// Returns [ErrNoEndRoot], which wraps [ErrInsufficientHistory], if the
	// history doesn't contain the [endRootID].
	GetChangeProof(
		ctx context.Context,
		startRootID ids.ID,
		endRootID ids.ID,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		maxLength int,
	) (*T, error)

	// Returns nil iff all the following hold:
	//   - [start] <= [end].
	//   - [proof] is non-empty.
	//   - All keys in [proof.KeyValues] and [proof.DeletedKeys] are in [start, end].
	//     If [start] is nothing, all keys are considered > [start].
	//     If [end] is nothing, all keys are considered < [end].
	//   - [proof.KeyValues] and [proof.DeletedKeys] are sorted in order of increasing key.
	//   - [proof.StartProof] and [proof.EndProof] are well-formed.
	//   - When the changes in [proof.KeyChanges] are applied,
	//     the root ID of the database is [expectedEndRootID].
	VerifyChangeProof(
		ctx context.Context,
		proof *T,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		expectedEndRootID ids.ID,
		maxKeys int,
	) error

	// CommitChangeProof commits the key/value pairs within the [proof] to the db.
	CommitChangeProof(ctx context.Context, end maybe.Maybe[[]byte], proof *T) (maybe.Maybe[[]byte], error)
}

type RangeProofer[T any] interface {
	// GetRangeProofAtRoot returns a proof for the key/value pairs in this trie within the range
	// [start, end] when the root of the trie was [rootID].
	// If [start] is Nothing, there's no lower bound on the range.
	// If [end] is Nothing, there's no upper bound on the range.
	// Returns ErrEmptyProof if [rootID] is ids.Empty.
	// Note that [rootID] == ids.Empty means the trie is empty
	// (i.e. we don't need a range proof.)
	GetRangeProofAtRoot(
		ctx context.Context,
		rootID ids.ID,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		maxLength int,
	) (*T, error)

	// Returns nil iff all the following hold:
	//   - [start] <= [end].
	//   - [proof] is non-empty.
	//   - All keys in [proof.KeyValues] and [proof.DeletedKeys] are in [start, end].
	//     If [start] is nothing, all keys are considered > [start].
	//     If [end] is nothing, all keys are considered < [end].
	//   - [proof.KeyValues] and [proof.DeletedKeys] are sorted in order of increasing key.
	//   - [proof.StartProof] and [proof.EndProof] are well-formed.
	//   - When the changes in [proof.KeyChanges] are applied,
	//     the root ID of the database is [expectedEndRootID].
	VerifyRangeProof(
		ctx context.Context,
		proof *T,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		expectedEndRootID ids.ID,
		maxKeys int,
	) error

	// CommitRangeProof commits the key/value pairs within the [proof] to the db.
	// [start] is the smallest possible key in the range this [proof] covers.
	// [end] is the largest possible key in the range this [proof] covers.
	CommitRangeProof(ctx context.Context, start, end maybe.Maybe[[]byte], proof *T) (maybe.Maybe[[]byte], error)
}
