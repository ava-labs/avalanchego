package flatfirewood

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestRowKey(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	for _, height := range []uint64{3, 7, 12} {
		require.NoError(db.Put(rowKey('A', []byte("key"), height), fmt.Appendf(nil, "value at %d", height)))
	}

	iter := db.NewIteratorWithPrefix([]byte{'A'})
	require.NotNil(iter)
	defer iter.Release()

	for _, height := range []uint64{12, 7, 3} {
		require.True(iter.Next())
		require.Equal(height, blockFromRowKey(iter.Key()))
		require.Equal(fmt.Appendf(nil, "value at %d", height), iter.Value())
	}
	require.False(iter.Next())
	require.NoError(iter.Error())
}

func TestLatestRowLE(t *testing.T) {
	const prefix = 'A'
	key := []byte("key")
	heights := []uint64{3, 7, 12}

	tests := []struct {
		name        string
		targetBlock uint64
		wantOK      bool
		wantBlock   uint64
		wantValue   []byte
	}{
		{
			name:        "before_first_row",
			targetBlock: 2,
		},
		{
			name:        "exact_match",
			targetBlock: 3,
			wantOK:      true,
			wantBlock:   3,
			wantValue:   []byte("value at 3"),
		},
		{
			name:        "between_rows",
			targetBlock: 4,
			wantOK:      true,
			wantBlock:   3,
			wantValue:   []byte("value at 3"),
		},
		{
			name:        "after_last_row",
			targetBlock: 100,
			wantOK:      true,
			wantBlock:   12,
			wantValue:   []byte("value at 12"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := memdb.New()
			for _, height := range heights {
				require.NoError(t, db.Put(rowKey(prefix, key, height), fmt.Appendf(nil, "value at %d", height)))
			}

			store := &store{db: db}
			value, block, ok, err := store.latestRowLE(prefix, key, test.targetBlock)
			require.NoError(t, err)
			require.Equal(t, test.wantOK, ok)
			require.Equal(t, test.wantBlock, block)
			require.Equal(t, test.wantValue, value)
		})
	}
}
