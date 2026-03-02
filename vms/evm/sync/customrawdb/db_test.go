// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"
)

func TestParseStateScheme(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Provided Firewood on empty disk -> allowed.
	scheme, err := ParseStateScheme(FirewoodScheme, db)
	require.NoError(t, err)
	require.Equal(t, FirewoodScheme, scheme)

	// Simulate disk has non-empty path scheme by writing persistent state id.
	rawdb.WritePersistentStateID(db, 1)
	scheme, err = ParseStateScheme(FirewoodScheme, db)
	require.ErrorIs(t, err, errStateSchemeConflict)
	require.Empty(t, scheme)

	// Pass-through to rawdb for non-Firewood using a fresh empty DB.
	db2 := rawdb.NewMemoryDatabase()
	scheme, err = ParseStateScheme(rawdb.HashScheme, db2)
	require.NoError(t, err)
	require.Equal(t, rawdb.HashScheme, scheme)
}
