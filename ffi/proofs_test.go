// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const maxProofLen = 10

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

func TestRangeProofEmptyDB(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	proof, err := db.RangeProof(EmptyRoot, nothing(), nothing(), 0)
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

	proof, err := db.RangeProof(root, nothing(), nothing(), 0)
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
	proof1 := rangeProof(t, db, root, nothing(), nothing())

	// get a proof over a different range
	proof2 := rangeProof(t, db, root, something([]byte("key2")), something([]byte("key3")))

	// ensure the proofs are different
	r.NotEqual(proof1, proof2)

	// TODO(https://github.com/ava-labs/firewood/issues/738): verify the proofs
}

func TestRangeProofDiffersAfterUpdate(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Insert some data.
	keys, vals := kvForTest(100)
	root1, err := db.Update(keys[:50], vals[:50])
	r.NoError(err)

	// get a proof
	proof := rangeProof(t, db, root1, nothing(), nothing())

	// insert more data
	root2, err := db.Update(keys[50:], vals[50:])
	r.NoError(err)
	r.NotEqual(root1, root2)

	// get a proof again
	proof2 := rangeProof(t, db, root2, nothing(), nothing())

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
	proofBytes := rangeProof(t, db, root, nothing(), nothing())

	// Deserialize the proof.
	proof := new(RangeProof)
	err = proof.UnmarshalBinary(proofBytes)
	r.NoError(err)

	// serialize the proof again
	serialized, err := proof.MarshalBinary()
	r.NoError(err)
	r.Equal(proofBytes, serialized)

	r.NoError(proof.Free())
}

// rangeProof generates a range proof for the given parameters.
func rangeProof(
	t *testing.T,
	db *Database,
	root Hash,
	startKey, endKey maybe,
) []byte {
	r := require.New(t)

	proof, err := db.RangeProof(root, startKey, endKey, maxProofLen)
	r.NoError(err)
	r.NotNil(proof)
	proofBytes, err := proof.MarshalBinary()
	r.NoError(err)
	r.NoError(proof.Free())

	return proofBytes
}
