// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"testing"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

func TestReconstructedReaderGet(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Commit initial state with a key-value pair.
	key := []byte("testkey")
	value := []byte("testvalue")
	root, err := db.Firewood.Update([]ffi.BatchOp{ffi.Put(key, value)})
	require.NoError(t, err)

	// Get a revision at the committed root.
	rev, err := db.Firewood.Revision(root)
	require.NoError(t, err)

	// Reconstruct with an additional key.
	key2 := []byte("testkey2")
	value2 := []byte("testvalue2")
	recon, err := rev.Reconstruct([]ffi.BatchOp{ffi.Put(key2, value2)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recon.Drop()) })

	// Wrap in reconstructedReader and verify both keys are readable.
	reader := &reconstructedReader{reconstructed: recon}

	got, err := reader.Node(common.Hash{}, key, common.Hash{})
	require.NoError(t, err)
	require.Equal(t, value, got)

	got, err = reader.Node(common.Hash{}, key2, common.Hash{})
	require.NoError(t, err)
	require.Equal(t, value2, got)

	// Non-existent key returns nil.
	got, err = reader.Node(common.Hash{}, []byte("missing"), common.Hash{})
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestReconstructedReaderFromRevision(t *testing.T) {
	config := DefaultConfig(t.TempDir())
	db, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	key := []byte("hello")
	value := []byte("world")
	root, err := db.Firewood.Update([]ffi.BatchOp{ffi.Put(key, value)})
	require.NoError(t, err)

	rev, err := db.Firewood.Revision(root)
	require.NoError(t, err)

	reader, recon, err := newReconstructedReaderFromRevision(rev, nil)
	require.NoError(t, err)
	require.NotNil(t, reader)
	t.Cleanup(func() { require.NoError(t, recon.Drop()) })

	got, err := reader.Node(common.Hash{}, key, common.Hash{})
	require.NoError(t, err)
	require.Equal(t, value, got)
}
