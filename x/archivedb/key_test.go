// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"bytes"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNaturalDescSortingForSameKey(t *testing.T) {
	key0, _ := newDBKeyFromUser(nil, 0)
	key1, _ := newDBKeyFromUser(nil, 1)
	key2, _ := newDBKeyFromUser(nil, 2)
	key3, _ := newDBKeyFromUser(nil, 3)

	entry := [][]byte{key0, key1, key2, key3}
	expected := [][]byte{key3, key2, key1, key0}

	slices.SortFunc(entry, bytes.Compare)

	require.Equal(t, expected, entry)
}

func TestSortingDifferentPrefix(t *testing.T) {
	key0, _ := newDBKeyFromUser([]byte{0}, 0)
	key1, _ := newDBKeyFromUser([]byte{0}, 1)
	key2, _ := newDBKeyFromUser([]byte{1}, 0)
	key3, _ := newDBKeyFromUser([]byte{1}, 1)

	entry := [][]byte{key0, key1, key2, key3}
	expected := [][]byte{key1, key0, key3, key2}

	slices.SortFunc(entry, bytes.Compare)

	require.Equal(t, expected, entry)
}

func TestParseDBKey(t *testing.T) {
	require := require.New(t)

	key := []byte{0, 1, 2, 3, 4, 5}
	height := uint64(102310)
	dbKey, _ := newDBKeyFromUser(key, height)

	parsedKey, parsedHeight, err := parseDBKeyFromUser(dbKey)
	require.NoError(err)
	require.Equal(key, parsedKey)
	require.Equal(height, parsedHeight)
}

func FuzzMetadataKeyInvariant(f *testing.F) {
	f.Fuzz(func(t *testing.T, userKey []byte, metadataKey []byte) {
		// The prefix is independent of the height, so its value doesn't matter
		// for this test.
		_, dbKeyPrefix := newDBKeyFromUser(userKey, 0)
		dbKey := newDBKeyFromMetadata(metadataKey)
		require.False(t, bytes.HasPrefix(dbKey, dbKeyPrefix))
	})
}
