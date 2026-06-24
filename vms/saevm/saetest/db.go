// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

// CopyDB returns an in-memory copy of src, used to hand a fresh VM the persisted
// state of a prior one without sharing the live database.
func CopyDB(tb testing.TB, src database.Database) database.Database {
	tb.Helper()

	dst := memdb.New()
	it := src.NewIterator()
	defer it.Release()
	for it.Next() {
		require.NoErrorf(tb, dst.Put(it.Key(), it.Value()), "%T.Put() during database copy", dst)
	}
	require.NoErrorf(tb, it.Error(), "%T.Error() after database copy", it)
	return dst
}
