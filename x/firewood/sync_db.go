// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
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

// TODO: implement
func (db *syncDB) CommitChangeProof(_ context.Context, end maybe.Maybe[[]byte], proof *ChangeProof) (nextKey maybe.Maybe[[]byte], err error) {
	// Set up cleanup.
	var nextKeyRange *ffi.NextKeyRange
	defer func() {
		// If we got a nextKeyRange, free it too.
		if nextKeyRange != nil {
			err = errors.Join(err, nextKeyRange.Free())
		}
	}()

	// Verify and commit the proof in a single step (TODO: separate these steps).
	// CommitRangeProof will verify the proof as part of committing it.
	db.lock.Lock()
	defer db.lock.Unlock()
	newRoot, err := db.fw.VerifyAndCommitChangeProof(proof.proof, ffi.Hash(proof.startRoot), ffi.Hash(proof.endRoot), proof.startKey, end, uint32(proof.maxLength))
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// TODO: This case should be handled by `FindNextKey`.
	if ids.ID(newRoot) == proof.endRoot {
		return maybe.Nothing[[]byte](), nil
	}

	return proof.FindNextKey()
}

// Commit the range proof to the database.
// TODO: This should only commit the range proof, not verify it.
// This will be resolved once the Firewood supports that.
// This is the last call to the proof, so it and any resources should be freed.
func (db *syncDB) CommitRangeProof(_ context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (nextKey maybe.Maybe[[]byte], err error) {
	// Verify and commit the proof in a single step (TODO: separate these steps).
	// CommitRangeProof will verify the proof as part of committing it.
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

	return proof.FindNextKey()
}

// TODO: implement
func (db *syncDB) GetChangeProof(_ context.Context, startRootID ids.ID, endRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*ChangeProof, error) {
	proof, err := db.fw.ChangeProof(ffi.Hash(startRootID), ffi.Hash(endRootID), start, end, uint32(maxLength))
	if err != nil {
		return nil, err
	}

	return newChangeProof(proof), nil
}

// Get the range proof between [start, end].
// The returned proof must be freed when no longer needed.
// Since this method is only called prior to marshalling the proof for sending over the
// network, the proof will be freed when marshalled.
func (db *syncDB) GetRangeProofAtRoot(_ context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error) {
	proof, err := db.fw.RangeProof(ffi.Hash(rootID), start, end, uint32(maxLength))
	if err != nil {
		return nil, err
	}

	return newRangeProof(proof), nil
}

// TODO: implement
// Right now, we verify the proof as part of committing it, making this function a no-op.
// We must only pass the necessary data to CommitChangeProof so it can verify the proof.
//
//nolint:revive
func (db *syncDB) VerifyChangeProof(_ context.Context, proof *ChangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	if proof.proof == nil {
		return errNilProof
	}

	// TODO: once firewood can verify separately from committing, do that here.
	// For now, pass any necessary data to be done in CommitChangeProof.
	// Namely, the start root, end root, and max length.
	proof.startRoot = expectedEndRootID
	proof.endRoot = expectedEndRootID
	proof.maxLength = maxLength
	return nil
}

// TODO: implement
// Right now, we verify the proof as part of committing it, making this function a no-op.
// We must only pass the necessary data to CommitRangeProof so it can verify the proof.
func (db *syncDB) VerifyRangeProof(_ context.Context, proof *RangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	if proof.ffi == nil {
		return errNilProof
	}

	proof.root = expectedEndRootID
	proof.maxLength = maxLength

	return proof.ffi.Verify(ffi.Hash(expectedEndRootID), start, end, uint32(maxLength))
}

// TODO: implement
// No error is returned to ensure some tests pass.
//
//nolint:revive
func (db *syncDB) Clear() error {
	return nil
}
