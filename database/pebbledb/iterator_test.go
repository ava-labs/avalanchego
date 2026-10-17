// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebbledb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
)

// Pebble may free the memory behind the last key/value pair during the Next
// call that returns false, so the iterator must not keep slices into it.
func TestIteratorDoneHoldsNoPebbleMemory(t *testing.T) {
	tests := []struct {
		name        string
		closeDB     bool
		expectedErr error
	}{
		{
			name: "exhausted",
		},
		{
			name:        "database closed",
			closeDB:     true,
			expectedErr: database.ErrClosed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			db := newDB(t)
			require.NoError(db.Put([]byte("key"), []byte("value")))

			it := db.NewIterator().(*iter)
			require.True(it.Next())
			if tt.closeDB {
				require.NoError(db.Close())
			}
			require.False(it.Next())
			require.Nil(it.nextKey)
			require.Nil(it.nextVal)

			it.Release()
			require.Nil(it.Key())
			require.Nil(it.Value())
			require.ErrorIs(it.Error(), tt.expectedErr)

			_ = db.Close()
		})
	}
}
