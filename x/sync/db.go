// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"encoding"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

var (
	_ DB[*merkledb.RangeProof, *merkledb.ChangeProof] = (merkledb.MerkleDB)(nil)

	_ Proof = (*merkledb.RangeProof)(nil)
	_ Proof = (*merkledb.ChangeProof)(nil)
)

type DB[TRange, TChange Proof] interface {
	GetMerkleRoot(ctx context.Context) (ids.ID, error)
	Clear() error
	ChangeProofer[TChange]
	RangeProofer[TRange]
}

type PProof[T any] interface {
	*T
	Proof
}
type Proof interface {
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler
}

type ChangeProofer[T Proof] interface {
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
	) (T, error)

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
		proof T,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		expectedEndRootID ids.ID,
		maxLength int,
	) error

	// CommitChangeProof commits the key/value pairs within the [proof] to the db.
	CommitChangeProof(ctx context.Context, end maybe.Maybe[[]byte], proof T) (maybe.Maybe[[]byte], error)
}

type RangeProofer[T Proof] interface {
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
	) (T, error)

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
		proof T,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		expectedEndRootID ids.ID,
		maxLength int,
	) error

	// CommitRangeProof commits the key/value pairs within the [proof] to the db.
	// [start] is the smallest possible key in the range this [proof] covers.
	// [end] is the largest possible key in the range this [proof] covers.
	CommitRangeProof(ctx context.Context, start, end maybe.Maybe[[]byte], proof T) (maybe.Maybe[[]byte], error)
}
