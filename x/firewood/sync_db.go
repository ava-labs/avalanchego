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
)

// syncDB wraps a Firewood FFI database to implement the xsync.DB interface.
type syncDB struct {
	fw   *ffi.Database
	lock sync.Mutex // TODO: remove this lock once FFI is thread-safe
}

func wrapSyncDB(db *ffi.Database) *syncDB {
	return &syncDB{fw: db}
}

func (db *syncDB) GetMerkleRoot(context.Context) (ids.ID, error) {
	root, err := db.fw.Root()
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ID(root), nil
}

// GetRangeProofAtRoot returns a range proof for x/sync between [start, end].
func (db *syncDB) GetRangeProofAtRoot(_ context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error) {
	proof, err := db.fw.RangeProof(ffi.Hash(rootID), start, end, uint32(maxLength))
	if err != nil {
		return nil, err
	}

	return &RangeProof{
		ffi: proof,
	}, nil
}

// VerifyRangeProof ensures the range proof matches the expected parameters.
// This does not require database state.
func (*syncDB) VerifyRangeProof(_ context.Context, proof *RangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	// Extra data must be provided at commit time.
	// TODO: remove this once the FFI no longer requires it.
	proof.root = expectedEndRootID
	proof.maxLength = maxLength

	return proof.ffi.Verify(ffi.Hash(expectedEndRootID), start, end, uint32(maxLength))
}

// Commit the range proof to the database.
func (db *syncDB) CommitRangeProof(_ context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error) {
	// Prevent concurrent commits to the database.
	db.lock.Lock()
	defer db.lock.Unlock()
	_, err := db.fw.VerifyAndCommitRangeProof(proof.ffi, start, end, ffi.Hash(proof.root), uint32(proof.maxLength))
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// We can now get the FindNextKey iterator.
	nextKeyRange, err := proof.ffi.FindNextKey()
	if err != nil || nextKeyRange == nil {
		// No error indicates the range is complete.
		return maybe.Nothing[[]byte](), err
	}

	// It is borrowed from the ffi, so we need to make our own copy.
	byteSlice := nextKeyRange.StartKey()
	newSlice := make([]byte, len(byteSlice))
	copy(newSlice, byteSlice)
	if err := nextKeyRange.Free(); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// TODO: This will eventually be handled by `FindNextKey`.
	if (end.HasValue() && bytes.Compare(newSlice, end.Value()) > 0) || (start.HasValue() && bytes.Equal(newSlice, start.Value())) {
		// There is no next key, the entire range has been committed.
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

//nolint:revive // TODO: implement this method.
func (db *syncDB) Clear() error {
	return errors.New("not implemented")
}
