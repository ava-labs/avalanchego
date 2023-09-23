// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.org/x/exp/slices"
)

func TestNaturalDescSortingForSameKey(t *testing.T) {
	key0, _ := newDBKey(make([]byte, 0), 0)
	key1, _ := newDBKey(make([]byte, 0), 1)
	key2, _ := newDBKey(make([]byte, 0), 2)
	key3, _ := newDBKey(make([]byte, 0), 3)

	entry := [][]byte{key0, key1, key2, key3}
	expected := [][]byte{key3, key2, key1, key0}

	slices.SortFunc(entry, func(i, j []byte) bool {
		return bytes.Compare(i, j) < 0
	})

	require.Equal(t, expected, entry)
}

func TestSortingDifferentPrefix(t *testing.T) {
	key0, _ := newDBKey([]byte{0}, 0)
	key1, _ := newDBKey([]byte{0}, 1)
	key2, _ := newDBKey([]byte{1}, 0)
	key3, _ := newDBKey([]byte{1}, 1)

	entry := [][]byte{key0, key1, key2, key3}
	expected := [][]byte{key1, key0, key3, key2}

	slices.SortFunc(entry, func(i, j []byte) bool {
		return bytes.Compare(i, j) < 0
	})

	require.Equal(t, expected, entry)
}

func TestParseDBKey(t *testing.T) {
	require := require.New(t)

	key := []byte{0, 1, 2, 3, 4, 5}
	height := uint64(102310)
	dbKey, _ := newDBKey(key, height)

	parsedKey, parsedHeight, err := parseDBKey(dbKey)
	require.NoError(err)
	require.Equal(key, parsedKey)
	require.Equal(height, parsedHeight)
}
