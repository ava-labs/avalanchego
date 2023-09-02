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
	key0 := newKey(make([]byte, 0), 0)
	key1 := newKey(make([]byte, 0), 1)
	key2 := newKey(make([]byte, 0), 2)
	key3 := newKey(make([]byte, 0), 3)

	entry := [][]byte{key0.Bytes(), key1.Bytes(), key2.Bytes(), key3.Bytes()}
	expected := [][]byte{key3.Bytes(), key2.Bytes(), key1.Bytes(), key0.Bytes()}

	slices.SortFunc(entry, func(i, j []byte) bool {
		return bytes.Compare(i, j) < 0
	})

	require.Equal(t, expected, entry)
}

func TestSortingDifferentPrefix(t *testing.T) {
	key0 := newKey([]byte{0}, 0)
	key1 := newKey([]byte{0}, 1)
	key2 := newKey([]byte{1}, 0)
	key3 := newKey([]byte{1}, 1)

	entry := [][]byte{key0.Bytes(), key1.Bytes(), key2.Bytes(), key3.Bytes()}
	expected := [][]byte{key1.Bytes(), key0.Bytes(), key3.Bytes(), key2.Bytes()}

	slices.SortFunc(entry, func(i, j []byte) bool {
		return bytes.Compare(i, j) < 0
	})

	require.Equal(t, expected, entry)
}

func TestDeleteKeyIsDifferent(t *testing.T) {
	key0 := newKey([]byte{0}, 0)
	key1 := newKey([]byte{0}, 0)

	require.Equal(t, key0.Bytes(), key1.Bytes())
	key1.isDeleted = true
	require.NotEqual(t, key0.Bytes(), key1.Bytes())
}

func TestParseBack(t *testing.T) {
	key0 := newKey([]byte{0, 1, 2, 3, 4, 5}, 102310)
	key1, err := parseKey(key0.Bytes())
	require.NoError(t, err)
	require.Equal(t, key0, key1)
	key0.isDeleted = true
	key1, err = parseKey(key0.Bytes())
	require.NoError(t, err)
	require.Equal(t, key0, key1)
	require.Equal(t, key1.prefix, []byte{0, 1, 2, 3, 4, 5})
	require.Equal(t, key1.height, uint64(102310))
}
