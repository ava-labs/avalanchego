// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ xsync.DB[*RangeProof, *ChangeProof] = (*syncDB)(nil)

	errNilProof = errors.New("nil proof")
)

type syncDB struct {
	fw   *ffi.Database
	lock sync.Mutex
}

func New(db *ffi.Database) *syncDB {
	return &syncDB{fw: db}
}

func (db *syncDB) GetMerkleRoot(context.Context) (ids.ID, error) {
	root, err := db.fw.Root()
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ID(root), nil
}

// GetRangeProof returns a range proof for x/sync between [start, end].
func (db *syncDB) GetRangeProofAtRoot(_ context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error) {
	proof, err := db.fw.RangeProof(ffi.Hash(rootID), start, end, uint32(maxLength))
	if err != nil {
		return nil, err
	}

	return newRangeProof(proof), nil
}

// VerifyRangeProof ensures the range proof matches the expected parameters.
func (*syncDB) VerifyRangeProof(_ context.Context, proof *RangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	if proof.ffi == nil {
		return errNilProof
	}

	proof.root = expectedEndRootID
	proof.maxLength = maxLength

	return proof.ffi.Verify(ffi.Hash(expectedEndRootID), start, end, uint32(maxLength))
}

// Commit the range proof to the database.
func (db *syncDB) CommitRangeProof(_ context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error) {
	// Prevent concurrent commits to the database.
	db.lock.Lock()
	defer db.lock.Unlock()
	newRoot, err := db.fw.VerifyAndCommitRangeProof(proof.ffi, start, end, ffi.Hash(proof.root), uint32(proof.maxLength))
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// TODO: This case should be handled by `FindNextKey`.
	if ids.ID(newRoot) == proof.root {
		return maybe.Nothing[[]byte](), nil
	}

	// We can now get the FindNextKey iterator.
	nextKeyRange, err := proof.ffi.FindNextKey()
	if err != nil || nextKeyRange == nil {
		// Indicates the range is complete.
		return maybe.Nothing[[]byte](), err
	}

	byteSlice := nextKeyRange.StartKey()
	newSlice := make([]byte, len(byteSlice))
	copy(newSlice, byteSlice)

	// Done using nextKeyRange
	if err := nextKeyRange.Free(); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// TODO: Instead of using bytes.Equal, we should ensure that newSlice is greater than (or equal to) start.
	// if start.HasValue() && bytes.Equal(newSlice, start.Value()) {
	if start.HasValue() && (bytes.Compare(newSlice, end.Value()) > 0 || bytes.Equal(newSlice, start.Value())) {
		// There is no next key.
		return maybe.Nothing[[]byte](), nil
	}

	return maybe.Some(newSlice), nil
}

//nolint:revive // TODO: implement this method.
func (db *syncDB) GetChangeProof(_ context.Context, startRootID ids.ID, endRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*ChangeProof, error) {
	return nil, errors.New("change proofs are not implemented")
}

//nolint:revive // TODO: implement this method.
func (db *syncDB) VerifyChangeProof(_ context.Context, proof *ChangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], endRoot ids.ID, maxLength int) error {
	return errors.New("change proofs are not implemented")
}

//nolint:revive // TODO: implement this method.
func (db *syncDB) CommitChangeProof(_ context.Context, end maybe.Maybe[[]byte], proof *ChangeProof) (maybe.Maybe[[]byte], error) {
	return maybe.Nothing[[]byte](), errors.New("change proofs are not implemented")
}

// Clear the database.
//
//nolint:revive // TODO: implement this method. It is left nil to allow empty DBs to be used in tests.
func (db *syncDB) Clear() error {
	return nil
}
