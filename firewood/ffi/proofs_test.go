// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	rangeProofLenUnbounded = 0
	rangeProofLenTruncated = 10
)

type maybe struct {
	value    []byte
	hasValue bool
}

func (m maybe) HasValue() bool {
	return m.hasValue
}

func (m maybe) Value() []byte {
	return m.value
}

func something(b []byte) maybe {
	return maybe{
		hasValue: true,
		value:    b,
	}
}

func nothing() maybe {
	return maybe{
		hasValue: false,
	}
}

// assertProofNotNil verifies that the given proof and its inner handle are not nil.
func assertProofNotNil(t *testing.T, proof *RangeProof) {
	t.Helper()
	r := require.New(t)
	r.NotNil(proof)
	r.NotNil(proof.handle)
}

// newVerifiedRangeProof generates a range proof for the given parameters and
// verifies using [RangeProof.Verify] which does not prepare a proposal. A
// cleanup is registered to free the proof when the test ends.
func newVerifiedRangeProof(
	t *testing.T,
	db *Database,
	root Hash,
	startKey, endKey maybe,
	proofLen uint32,
) *RangeProof {
	r := require.New(t)

	proof, err := db.RangeProof(root, startKey, endKey, proofLen)
	r.NoError(err)
	assertProofNotNil(t, proof)
	t.Cleanup(func() { r.NoError(proof.Free()) })

	r.NoError(proof.Verify(root, startKey, endKey, proofLen))

	return proof
}

// newSerializedRangeProof generates a range proof for the given parameters and
// returns its serialized bytes.
func newSerializedRangeProof(
	t *testing.T,
	db *Database,
	root Hash,
	startKey, endKey maybe,
	proofLen uint32,
) []byte {
	r := require.New(t)

	proof := newVerifiedRangeProof(t, db, root, startKey, endKey, proofLen)

	proofBytes, err := proof.MarshalBinary()
	r.NoError(err)

	return proofBytes
}

func TestRangeProofEmptyDB(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	proof, err := db.RangeProof(EmptyRoot, nothing(), nothing(), rangeProofLenUnbounded)
	r.ErrorIs(err, errRevisionNotFound)
	r.Nil(proof)
}

func TestRangeProofNonExistentRoot(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// insert some data
	keys, vals := kvForTest(100)
	root, err := db.Update(keys, vals)
	r.NoError(err)

	// create a bogus root
	root[0] ^= 0xFF

	proof, err := db.RangeProof(root, nothing(), nothing(), rangeProofLenUnbounded)
	r.ErrorIs(err, errRevisionNotFound)
	r.Nil(proof)
}

func TestRangeProofPartialRange(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Insert a lot of data.
	keys, vals := kvForTest(10000)
	root, err := db.Update(keys, vals)
	r.NoError(err)

	// get a proof over some partial range
	proof1 := newSerializedRangeProof(t, db, root, nothing(), nothing(), rangeProofLenTruncated)

	// get a proof over a different range
	proof2 := newSerializedRangeProof(t, db, root, something([]byte("key2")), something([]byte("key3")), rangeProofLenTruncated)

	// ensure the proofs are different
	r.NotEqual(proof1, proof2)
}

func TestRangeProofDiffersAfterUpdate(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Insert some data.
	keys, vals := kvForTest(100)
	root1, err := db.Update(keys[:50], vals[:50])
	r.NoError(err)

	// get a proof
	proof := newSerializedRangeProof(t, db, root1, nothing(), nothing(), rangeProofLenTruncated)

	// insert more data
	root2, err := db.Update(keys[50:], vals[50:])
	r.NoError(err)
	r.NotEqual(root1, root2)

	// get a proof again
	proof2 := newSerializedRangeProof(t, db, root2, nothing(), nothing(), rangeProofLenTruncated)

	// ensure the proofs are different
	r.NotEqual(proof, proof2)
}

func TestRoundTripSerialization(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Insert some data.
	keys, vals := kvForTest(10)
	root, err := db.Update(keys, vals)
	r.NoError(err)

	// get a proof
	proofBytes := newSerializedRangeProof(t, db, root, nothing(), nothing(), rangeProofLenUnbounded)

	// Deserialize the proof.
	proof := new(RangeProof)
	err = proof.UnmarshalBinary(proofBytes)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(proof.Free()) })

	// serialize the proof again
	serialized, err := proof.MarshalBinary()
	r.NoError(err)
	r.Equal(proofBytes, serialized)
}

func TestRangeProofVerify(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals := kvForTest(100)
	root, err := db.Update(keys, vals)
	r.NoError(err)

	proof := newVerifiedRangeProof(t, db, root, nothing(), nothing(), rangeProofLenTruncated)

	// Database should be immediately closeable (no keep-alive)
	r.NoError(db.Close(oneSecCtx(t)))

	// Verify with wrong root should fail
	root[0] ^= 0xFF
	err = proof.Verify(root, nothing(), nothing(), rangeProofLenTruncated)

	// TODO(#738): re-enable after verification is implemented
	// r.Error(err, "Verification with wrong root should fail")
	r.NoError(err)
}

func TestVerifyAndCommitRangeProof(t *testing.T) {
	r := require.New(t)

	// Create source and target databases
	dbSource := newTestDatabase(t)
	dbTarget := newTestDatabase(t)

	// Populate source
	keys, vals := kvForTest(50)
	sourceRoot, err := dbSource.Update(keys, vals)
	r.NoError(err)

	proof := newVerifiedRangeProof(t, dbSource, sourceRoot, nothing(), nothing(), rangeProofLenUnbounded)

	// Verify and commit to target without previously calling db.VerifyRangeProof
	committedRoot, err := dbTarget.VerifyAndCommitRangeProof(proof, nothing(), nothing(), sourceRoot, rangeProofLenUnbounded)
	r.NoError(err)
	r.Equal(sourceRoot, committedRoot)

	// Verify all keys are now in target database
	for i, key := range keys {
		got, err := dbTarget.Get(key)
		r.NoError(err, "Get key %d", i)
		r.Equal(vals[i], got, "Value mismatch for key %d", i)
	}
}

func TestRangeProofFindNextKey(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals := kvForTest(100)
	root, err := db.Update(keys, vals)
	r.NoError(err)

	proof := newVerifiedRangeProof(t, db, root, nothing(), nothing(), rangeProofLenTruncated)

	// FindNextKey should fail before preparing a proposal or committing
	_, err = proof.FindNextKey()
	r.ErrorIs(err, errNotPrepared, "FindNextKey should fail on unverified proof")

	// Verify the proof
	r.NoError(db.VerifyRangeProof(proof, nothing(), nothing(), root, rangeProofLenTruncated))

	// Now FindNextKey should work
	nextRange, err := proof.FindNextKey()
	r.NoError(err)
	r.NotNil(nextRange)
	startKey := nextRange.StartKey()
	r.NotEmpty(startKey)
	startKey = append([]byte{}, startKey...) // copy to new slice to avoid use-after-free
	r.NoError(nextRange.Free())

	_, err = db.VerifyAndCommitRangeProof(proof, nothing(), nothing(), root, rangeProofLenTruncated)
	r.NoError(err)

	// FindNextKey should still work after commit
	nextRange, err = proof.FindNextKey()
	r.NoError(err)
	r.NotNil(nextRange)
	r.Equal(nextRange.StartKey(), startKey)
	r.NoError(nextRange.Free())
}

func TestRangeProofFreeReleasesKeepAlive(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals := kvForTest(50)
	root, err := db.Update(keys, vals)
	r.NoError(err)

	proof := newVerifiedRangeProof(t, db, root, nothing(), nothing(), rangeProofLenTruncated)
	r.NoError(err)

	// prepare proposal (acquires keep-alive)
	r.NoError(db.VerifyRangeProof(proof, nothing(), nothing(), root, rangeProofLenTruncated))

	// Database should not be closeable while proof has keep-alive
	r.ErrorIs(db.Close(oneSecCtx(t)), ErrActiveKeepAliveHandles)

	// Free the proof (releases keep-alive)
	r.NoError(proof.Free())

	// Database should now be closeable
	r.NoError(db.Close(oneSecCtx(t)))
}

func TestRangeProofCommitReleasesKeepAlive(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals := kvForTest(50)
	root, err := db.Update(keys, vals)
	r.NoError(err)

	proof := newVerifiedRangeProof(t, db, root, nothing(), nothing(), rangeProofLenTruncated)
	marshalledBeforeCommit, err := proof.MarshalBinary()
	r.NoError(err)

	// prepare proposal (acquires keep-alive)
	r.NoError(db.VerifyRangeProof(proof, nothing(), nothing(), root, rangeProofLenTruncated))

	// Database should not be closeable while proof has keep-alive
	r.ErrorIs(db.Close(oneSecCtx(t)), ErrActiveKeepAliveHandles)

	// Commit the proof (releases keep-alive)
	_, err = db.VerifyAndCommitRangeProof(proof, nothing(), nothing(), root, rangeProofLenTruncated)
	r.NoError(err)

	// Database should now be closeable
	r.NoError(db.Close(oneSecCtx(t)))

	marshalledAfterCommit, err := proof.MarshalBinary()
	r.NoError(err)

	// methods like MarshalBinary should still work after commit and closing the database
	r.Equal(marshalledBeforeCommit, marshalledAfterCommit)
}

// TestRangeProofFinalizerCleanup verifies that the finalizer properly releases
// the keep-alive handle when the proof goes out of scope.
func TestRangeProofFinalizerCleanup(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals := kvForTest(50)
	root, err := db.Update(keys, vals)
	r.NoError(err)

	// note: this does not use newVerifiedRangeProof because it sets a cleanup
	// which retains a handle to the proof blocking our ability to wait for the
	// finalizer to run
	proof, err := db.RangeProof(root, nothing(), nothing(), rangeProofLenTruncated)
	r.NoError(err)
	assertProofNotNil(t, proof)

	// prepare proposal (acquires keep-alive)
	r.NoError(db.VerifyRangeProof(proof, nothing(), nothing(), root, rangeProofLenTruncated))

	// Database should not be closeable while proof has keep-alive
	r.ErrorIs(db.Close(oneSecCtx(t)), ErrActiveKeepAliveHandles)

	runtime.KeepAlive(proof)
	proof = nil //nolint:ineffassign // necessary to drop the reference for GC
	runtime.GC()

	r.NoError(db.Close(t.Context()), "Database should be closeable after proof is garbage collected")
}
