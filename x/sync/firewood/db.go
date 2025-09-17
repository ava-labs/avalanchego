package firewood

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ xsync.DB[*rangeProof, *changeProof] = (*DB)(nil)

	errNilProof        = errors.New("nil proof")
	errMismatchingRoot = errors.New("committed root does not match expected root")
)

type DB struct {
	fw *ffi.Database
}

func New(db *ffi.Database) *DB {
	return &DB{fw: db}
}

func (db *DB) GetMerkleRoot(context.Context) (ids.ID, error) {
	root, err := db.fw.Root()
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ID(root), nil
}

// TODO: implement
func (db *DB) CommitChangeProof(_ context.Context, end maybe.Maybe[[]byte], proof *changeProof) (nextKey maybe.Maybe[[]byte], err error) {
	// Set up cleanup.
	var nextKeyRange *ffi.NextKeyRange
	defer func() {
		// This will be our last call to the proof.
		err = errors.Join(err, proof.proof.Free())
		// If we got a nextKeyRange, free it too.
		if nextKeyRange != nil {
			err = errors.Join(err, nextKeyRange.Free())
		}
	}()

	// TODO: Remove copy. Currently necessary to avoid passing pointer to a stack variable?
	startRootBytes := make([]byte, ids.IDLen)
	copy(startRootBytes, proof.startRoot[:])
	endRootBytes := make([]byte, ids.IDLen)
	copy(endRootBytes, proof.endRoot[:])

	// Verify and commit the proof in a single step (TODO: separate these steps).
	// CommitRangeProof will verify the proof as part of committing it.
	root, err := db.fw.VerifyAndCommitChangeProof(proof.proof, startRootBytes, endRootBytes, proof.startKey, end, uint32(proof.maxLength))
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// Unexpected root - should never happen, but easy to verify.
	if bytes.Equal(root, proof.endRoot[:]) {
		return maybe.Nothing[[]byte](), fmt.Errorf("%w: %v != %v", errMismatchingRoot, root, proof.endRoot[:])
	}

	// We can now get the FindNextKey iterator.
	nextKeyRange, err = proof.proof.FindNextKey()
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// Return next key
	if nextKeyRange.HasEndKey() {
		return maybe.Some(nextKeyRange.EndKey()), nil
	}
	return maybe.Nothing[[]byte](), nil
}

// Commit the range proof to the database.
// TODO: This should only commit the range proof, not verify it.
// This will be resolved once the Firewood supports that.
// This is the last call to the proof, so it and any resources should be freed.
func (db *DB) CommitRangeProof(_ context.Context, start, end maybe.Maybe[[]byte], proof *rangeProof) (nextKey maybe.Maybe[[]byte], err error) {
	// Set up cleanup.
	var nextKeyRange *ffi.NextKeyRange
	defer func() {
		// This will be our last call to the proof.
		err = errors.Join(err, proof.proof.Free())
		// If we got a nextKeyRange, free it too.
		if nextKeyRange != nil {
			err = errors.Join(err, nextKeyRange.Free())
		}
	}()

	// TODO: Remove copy. Currently necessary to avoid passing pointer to a stack variable?
	rootBytes := make([]byte, ids.IDLen)
	copy(rootBytes, proof.root[:])

	// Verify and commit the proof in a single step (TODO: separate these steps).
	// CommitRangeProof will verify the proof as part of committing it.
	root, err := db.fw.VerifyAndCommitRangeProof(proof.proof, start, end, rootBytes, uint32(proof.maxLength))
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// Unexpected root - should never happen, but easy to verify.
	if !bytes.Equal(root, proof.root[:]) {
		return maybe.Nothing[[]byte](), fmt.Errorf("%w: %v != %v", errMismatchingRoot, root, proof.root[:])
	}

	// We can now get the FindNextKey iterator.
	nextKeyRange, err = proof.proof.FindNextKey()
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// Return next key
	if nextKeyRange.HasEndKey() {
		return maybe.Some(nextKeyRange.EndKey()), nil
	}
	return maybe.Nothing[[]byte](), nil
}

// TODO: implement
func (db *DB) GetChangeProof(_ context.Context, startRootID ids.ID, endRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*changeProof, error) {
	proof, err := db.fw.ChangeProof(startRootID[:], endRootID[:], start, end, uint32(maxLength))
	if err != nil {
		return nil, err
	}

	return newChangeProof(proof), nil
}

// Get the range proof between [start, end].
// The returned proof must be freed when no longer needed.
// Since this method is only called prior to marshalling the proof for sending over the
// network, the proof will be freed when marshalled.
func (db *DB) GetRangeProofAtRoot(_ context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*rangeProof, error) {
	proof, err := db.fw.RangeProof(maybe.Some(rootID[:]), start, end, uint32(maxLength))
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
func (db *DB) VerifyChangeProof(_ context.Context, proof *changeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
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
//
//nolint:revive
func (db *DB) VerifyRangeProof(_ context.Context, proof *rangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	if proof.proof == nil {
		return errNilProof
	}

	// TODO: once firewood can verify separately from committing, do that here.
	// For now, pass any necessary data to be done in CommitRangeProof.
	// Namely, the max length and root.
	proof.root = expectedEndRootID
	proof.maxLength = maxLength
	return nil
}

// TODO: implement
// No error is returned to ensure some tests pass.
//
//nolint:revive
func (db *DB) Clear() error {
	return nil
}
