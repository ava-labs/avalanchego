// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type valueResultSet struct {
	key   []byte
	setAt uint64
	value []byte
}

func consumeAllValues(t *testing.T, i allKeysAtHeightIterator) []valueResultSet {
	defer i.Release()
	results := []valueResultSet{}
	for i.Next() {
		results = append(results, valueResultSet{i.Key(), i.Height(), i.Value()})
	}

	require.NoError(t, i.Error())

	return results
}

func TestGetAllValuesAtHeight(t *testing.T) {
	db := getDBWithState(t, [][]changes{
		{},
		{
			entry("prefix:key1", "value1@1"),
			entry("prefix:key2", "value2@1"),
		},
		{
			entry("prefix:key1", "value1@10"),
			entry("prefix:key2", "value2@10"),
		},
		{},
		{
			entry("prefix:key1", "value1@100"),
		},
		{},
		{
			entry("prefix:key1", "value1@1000"),
			entry("prefix:key2", "value2@1000"),
		},
		{
			entry("prefix:ac", "pac@10000"),
			entry("suffix:ac", "sac@10000"),
			entry("prefix:99", "p99@10000"),
		},
		{},
		{delete("prefix:99"), delete("prefix:ac")},
	})
	require.Equal(t, []valueResultSet{
		{[]byte("prefix:key1"), 7, []byte("value1@1000")},
		{[]byte("prefix:key2"), 7, []byte("value2@1000")},
	}, consumeAllValues(t, db.GetAllAtHeight([]byte("prefix:"), 10)))
	require.Equal(t, []valueResultSet{
		{[]byte("prefix:99"), 8, []byte("p99@10000")},
		{[]byte("prefix:ac"), 8, []byte("pac@10000")},
		{[]byte("prefix:key1"), 7, []byte("value1@1000")},
		{[]byte("prefix:key2"), 7, []byte("value2@1000")},
	}, consumeAllValues(t, db.GetAllAtHeight([]byte("prefix:"), 9)))
	require.Equal(t, []valueResultSet{
		{[]byte("prefix:99"), 8, []byte("p99@10000")},
		{[]byte("prefix:ac"), 8, []byte("pac@10000")},
		{[]byte("prefix:key1"), 7, []byte("value1@1000")},
		{[]byte("prefix:key2"), 7, []byte("value2@1000")},
	}, consumeAllValues(t, db.GetAllAtHeight([]byte("prefix:"), 8)))
	require.Equal(t, []valueResultSet{
		{[]byte("prefix:key1"), 5, []byte("value1@100")},
		{[]byte("prefix:key2"), 3, []byte("value2@10")},
	}, consumeAllValues(t, db.GetAllAtHeight([]byte("prefix:"), 5)))
	require.Equal(
		t,
		[]valueResultSet{},
		consumeAllValues(t, db.GetAllAtHeight([]byte("prefix:"), 1)),
	)
}
