// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
)

func TestDBPut(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
		val  []byte
		want []byte
	}{
		{
			name: "nil key",
			key:  nil,
			val:  []byte("foo"),
			want: []byte("foo"),
		},
		{
			name: "empty key",
			key:  []byte{},
			val:  []byte{},
			want: []byte{},
		},
		{
			name: "empty val",
			key:  []byte("foo"),
			val:  []byte{},
			want: []byte{},
		},
		{
			name: "nil val",
			key:  []byte("foo"),
			val:  nil,
			want: []byte{},
		},
		{
			name: "non-empty val",
			key:  []byte("foo"),
			val:  []byte("bar"),
			want: []byte("bar"),
		},
		{
			name: "write to reserved key",
			key:  Prefix(reservedPrefix, heightKey),
			val:  []byte("foo"),
			want: []byte("foo"),
		},
		{
			name: "key is prefix delimiter",
			key:  []byte{PrefixDelimiter},
			val:  []byte("foo"),
			want: []byte("foo"),
		},
		{
			name: "key has prefix delimiter",
			key:  []byte("foo" + string(PrefixDelimiter)),
			val:  []byte("foo"),
			want: []byte("foo"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := New(filepath.Join(t.TempDir(), "firewood.test"))
			require.NoError(t, err)

			db.Put(tt.key, tt.val)

			got, err := db.Get(tt.key)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)

			require.NoError(t, db.Flush())

			got, err = db.Get(tt.key)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDBDelete(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "firewood.test"))
	require.NoError(t, err)

	key := []byte("foo")
	db.Put(key, []byte("bar"))
	db.Delete([]byte("foo"))

	_, err = db.Get(key)
	require.ErrorIs(t, err, database.ErrNotFound)

	require.NoError(t, db.Flush())
	_, err = db.Get(key)
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestDBHeight(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "firewood.test"))
	require.NoError(t, err)

	_, ok := db.Height()
	require.False(t, ok)

	for i := range 5 {
		db.Put([]byte("foo"), []byte("bar"))
		require.NoError(t, db.Flush())

		height, ok := db.Height()
		require.True(t, ok)
		require.Equal(t, uint64(i), height)
	}
}

func TestDBPersistence(t *testing.T) {
	dir := t.TempDir()

	db, err := New(filepath.Join(dir, "firewood.test"))
	require.NoError(t, err)

	key := []byte("foo")
	val := []byte("bar")
	db.Put(key, val)

	require.NoError(t, db.Flush())

	height, ok := db.Height()
	require.True(t, ok)
	require.Zero(t, height)

	got, err := db.Get(key)
	require.NoError(t, err)
	require.Equal(t, val, got)

	require.NoError(t, db.Close(t.Context()))

	db, err = New(filepath.Join(dir, "firewood.test"))
	require.NoError(t, err)

	height, ok = db.Height()
	require.True(t, ok)
	require.Zero(t, height)

	got, err = db.Get(key)
	require.NoError(t, err)
	require.Equal(t, val, got)
}

func TestPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix []byte
		key    []byte
		want   []byte
	}{
		{
			name:   "nil prefix",
			prefix: nil,
			key:    []byte("foo"),
			want:   []byte("/foo"),
		},
		{
			name:   "empty prefix",
			prefix: []byte{},
			key:    []byte("foo"),
			want:   []byte("/foo"),
		},
		{
			name:   "non-empty prefix",
			prefix: []byte("foo"),
			key:    []byte("bar"),
			want:   []byte("foo/bar"),
		},
		{
			name:   "prefix is delimiter",
			prefix: []byte{PrefixDelimiter},
			key:    []byte("bar"),
			want:   []byte("//bar"),
		},
		{
			name:   "prefix contains delimiter",
			prefix: []byte("foo" + string(PrefixDelimiter)),
			key:    []byte("bar"),
			want:   []byte("foo//bar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Prefix(tt.prefix, tt.key))
		})
	}
}

func TestDBAbort_Put(t *testing.T) {
	type put struct {
		key []byte
		val []byte
	}

	tests := []struct {
		name       string
		puts       []put
		abortedPut put
		wantVal    []byte
		wantErr    error
	}{
		{
			name:       "abort key create",
			abortedPut: put{key: []byte("foo"), val: []byte("bar")},
			wantErr:    database.ErrNotFound,
		},
		{
			name: "abort key update",
			puts: []put{
				{key: []byte("foo"), val: []byte("bar")},
			},
			abortedPut: put{key: []byte("foo"), val: []byte("bar")},
			wantVal:    []byte("bar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := New(filepath.Join(t.TempDir(), "firewood.test"))
			require.NoError(t, err)

			for _, p := range tt.puts {
				db.Put(p.key, p.val)
			}

			require.NoError(t, db.Flush())

			db.Put(tt.abortedPut.key, tt.abortedPut.val)
			db.Abort()

			gotVal, err := db.Get(tt.abortedPut.key)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantVal, gotVal)
		})
	}
}

func TestDBAbort_Delete(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "firewood.test"))
	require.NoError(t, err)

	key := []byte("foo")
	val := []byte("bar")

	db.Put(key, val)

	require.NoError(t, db.Flush())

	db.Delete(key)
	db.Abort()

	gotVal, err := db.Get(key)
	require.NoError(t, err)
	require.Equal(t, val, gotVal)
}

func TestDBRoot(t *testing.T) {
	type put struct {
		key []byte
		val []byte
	}

	tests := []struct {
		name string
		puts []put
	}{
		{
			name: "no puts",
		},
		{
			name: "single put",
			puts: []put{
				{key: []byte("foo"), val: []byte("bar")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := New(filepath.Join(t.TempDir(), "firewood.test"))
			require.NoError(t, err)

			prevRoot, err := db.Root()
			require.NoError(t, err)

			for _, p := range tt.puts {
				db.Put(p.key, p.val)
			}

			require.NoError(t, db.Flush())

			updatedRoot, err := db.Root()
			require.NoError(t, err)

			require.NotEqual(t, prevRoot, updatedRoot)
		})
	}
}
