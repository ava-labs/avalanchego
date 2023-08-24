package archivedb

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
)

func TestKeyHeightAndReversedHeight(t *testing.T) {
	x, err := NewKey(make([]byte, 0), 1000)
	require.NoError(t, err)
	require.True(t, x.HeightReversed > x.Height)
	require.Equal(t, uint64(1000), x.Height)
}

func TestNaturalDescSortingForSameKey(t *testing.T) {
	key0, err := NewKey(make([]byte, 0), 0)
	require.NoError(t, err)
	key1, err := NewKey(make([]byte, 0), 1)
	require.NoError(t, err)
	key2, err := NewKey(make([]byte, 0), 2)
	require.NoError(t, err)
	key3, err := NewKey(make([]byte, 0), 3)
	require.NoError(t, err)

	entry := [][]byte{key0.Bytes, key1.Bytes, key2.Bytes, key3.Bytes}
	expected := [][]byte{key3.Bytes, key2.Bytes, key1.Bytes, key0.Bytes}

	slices.SortFunc(entry, func(i, j []byte) bool {
		return bytes.Compare(i, j) < 0
	})

	require.Equal(t, expected, entry)
}

func TestSortingDifferentPrefix(t *testing.T) {
	key0, err := NewKey([]byte{0}, 0)
	require.NoError(t, err)
	key1, err := NewKey([]byte{0}, 1)
	require.NoError(t, err)
	key2, err := NewKey([]byte{1}, 0)
	require.NoError(t, err)
	key3, err := NewKey([]byte{1}, 1)
	require.NoError(t, err)

	entry := [][]byte{key0.Bytes, key1.Bytes, key2.Bytes, key3.Bytes}
	expected := [][]byte{key1.Bytes, key0.Bytes, key3.Bytes, key2.Bytes}

	slices.SortFunc(entry, func(i, j []byte) bool {
		return bytes.Compare(i, j) < 0
	})

	require.Equal(t, expected, entry)
}
