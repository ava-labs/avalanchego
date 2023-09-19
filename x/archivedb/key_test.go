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
	key0 := newDBKey(make([]byte, 0), 0)
	key1 := newDBKey(make([]byte, 0), 1)
	key2 := newDBKey(make([]byte, 0), 2)
	key3 := newDBKey(make([]byte, 0), 3)

	entry := [][]byte{key0, key1, key2, key3}
	expected := [][]byte{key3, key2, key1, key0}

	slices.SortFunc(entry, func(i, j []byte) bool {
		return bytes.Compare(i, j) < 0
	})

	require.Equal(t, expected, entry)
}

func TestSortingDifferentPrefix(t *testing.T) {
	key0 := newDBKey([]byte{0}, 0)
	key1 := newDBKey([]byte{0}, 1)
	key2 := newDBKey([]byte{1}, 0)
	key3 := newDBKey([]byte{1}, 1)

	entry := [][]byte{key0, key1, key2, key3}
	expected := [][]byte{key1, key0, key3, key2}

	slices.SortFunc(entry, func(i, j []byte) bool {
		return bytes.Compare(i, j) < 0
	})

	require.Equal(t, expected, entry)
}

func TestParseBack(t *testing.T) {
	keyBytes := []byte{0, 1, 2, 3, 4, 5}
	keyHeight := uint64(102310)
	key0 := newDBKey(keyBytes, keyHeight)
	key, height, err := parseDBKey(key0)
	require.NoError(t, err)
	require.Equal(t, key, keyBytes)
	require.Equal(t, height, keyHeight)
}
