// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
)

func Test_Snapshot_Basic(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)
	ctx := context.Background()
	view, err := db.NewView(
		ctx,
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: []byte{0}, Value: []byte{0}},
			},
		},
	)
	require.NoError(err)
	require.NoError(view.CommitToDB(ctx))

	view, err = db.NewView(
		ctx,
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: []byte{1}, Value: []byte{1}},
			},
		},
	)
	require.NoError(err)
	require.NoError(view.CommitToDB(ctx))

	snap, err := db.NewSnapshot(3)
	require.NoError(err)

	// confirm that the snapshot has the expected values
	val, err := snap.GetValue(ctx, []byte{0})
	require.NoError(err)
	require.Equal([]byte{0}, val)

	val, err = snap.GetValue(ctx, []byte{1})
	require.NoError(err)
	require.Equal([]byte{1}, val)

	// commit new key/value
	view, err = db.NewView(
		ctx,
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: []byte{3}, Value: []byte{3}},
			},
		},
	)
	require.NoError(err)
	require.NoError(view.CommitToDB(ctx))

	// the snapshot should not contain the new value
	_, err = snap.GetValue(ctx, []byte{3})
	require.ErrorIs(err, database.ErrNotFound)

	// write over existing key values
	view, err = db.NewView(
		ctx,
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: []byte{1}, Value: []byte{4}},
			},
		},
	)
	require.NoError(err)
	require.NoError(view.CommitToDB(ctx))

	// should still have the old value
	val, err = snap.GetValue(ctx, []byte{1})
	require.NoError(err)
	require.Equal([]byte{1}, val)

	// commit more to trigger view invalidation
	view, err = db.NewView(
		ctx,
		ViewChanges{
			BatchOps: []database.BatchOp{
				{Key: []byte{4}, Value: []byte{4}},
			},
		},
	)
	require.NoError(err)
	require.NoError(view.CommitToDB(ctx))

	// too many revisions have passed and now the snapshot is invalid
	_, err = snap.GetValue(ctx, []byte{1})
	require.ErrorIs(err, ErrInvalid)
}
