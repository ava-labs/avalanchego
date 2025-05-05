package firewood

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// The cgocheck debugging flag checks that all pointers are pinned.
	// TODO(arr4n) why doesn't `//go:debug cgocheck=1` work? https://go.dev/doc/godebug
	debug := strings.Split(os.Getenv("GODEBUG"), ",")
	var hasCgoCheck bool
	for _, kv := range debug {
		switch strings.TrimSpace(kv) {
		case "cgocheck=1":
			hasCgoCheck = true
			break
		case "cgocheck=0":
			fmt.Fprint(os.Stderr, "GODEBUG=cgocheck=0; MUST be 1 for Firewood cgo tests")
			os.Exit(1)
		}
	}

	if !hasCgoCheck {
		debug = append(debug, "cgocheck=1")
		if err := os.Setenv("GODEBUG", strings.Join(debug, ",")); err != nil {
			fmt.Fprintf(os.Stderr, `os.Setenv("GODEBUG", ...) error %v`, err)
			os.Exit(1)
		}
	}

	os.Exit(m.Run())
}

func newTestDatabase(t *testing.T) *Database {
	t.Helper()

	conf := DefaultConfig()
	conf.Create = true
	// The TempDir directory is automatically cleaned up so there's no need to
	// remove test.db.
	dbFile := filepath.Join(t.TempDir(), "test.db")

	f, err := New(dbFile, conf)
	require.NoErrorf(t, err, "NewDatabase(%+v)", conf)
	// Close() always returns nil, its signature returning an error only to
	// conform with an externally required interface.
	t.Cleanup(func() { f.Close() })
	return f
}

func TestInsert(t *testing.T) {
	db := newTestDatabase(t)
	const (
		key = "abc"
		val = "def"
	)
	db.Batch([]KeyValue{
		{[]byte(key), []byte(val)},
	})

	got, err := db.Get([]byte(key))
	require.NoErrorf(t, err, "%T.Get(%q)", db, key)
	assert.Equal(t, val, string(got), "Recover lone batch-inserted value")
}

func TestGetNonExistent(t *testing.T) {
	db := newTestDatabase(t)
	got, err := db.Get([]byte("non-existent"))
	require.NoError(t, err)
	assert.Nil(t, got)
}

// Attempt to make a call to a nil or invalid handle.
// Each function should return an error and not panic.
func TestGetBadHandle(t *testing.T) {
	db := &Database{handle: nil}

	// This ignores error, but still shouldn't panic.
	_, err := db.Get([]byte("non-existent"))
	assert.ErrorIs(t, err, dbClosedErr)

	// We ignore the error, but it shouldn't panic.
	_, err = db.Root()
	assert.ErrorIs(t, err, dbClosedErr)

	root, err := db.Update(
		[][]byte{[]byte("key")},
		[][]byte{[]byte("value")},
	)
	assert.Empty(t, root)
	assert.ErrorIs(t, err, dbClosedErr)

	err = db.Close()
	require.ErrorIs(t, err, dbClosedErr)
}

func keyForTest(i int) []byte {
	return []byte("key" + strconv.Itoa(i))
}

func valForTest(i int) []byte {
	return []byte("value" + strconv.Itoa(i))
}

func kvForTest(i int) KeyValue {
	return KeyValue{
		Key:   keyForTest(i),
		Value: valForTest(i),
	}
}

func TestInsert100(t *testing.T) {
	tests := []struct {
		name   string
		insert func(*Database, []KeyValue) (root []byte, _ error)
	}{
		{
			name: "Batch",
			insert: func(db *Database, kvs []KeyValue) ([]byte, error) {
				return db.Batch(kvs)
			},
		},
		{
			name: "Update",
			insert: func(db *Database, kvs []KeyValue) ([]byte, error) {
				var keys, vals [][]byte
				for _, kv := range kvs {
					keys = append(keys, kv.Key)
					vals = append(vals, kv.Value)
				}
				return db.Update(keys, vals)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDatabase(t)

			ops := make([]KeyValue, 100)
			for i := range ops {
				ops[i] = kvForTest(i)
			}
			rootFromInsert, err := tt.insert(db, ops)
			require.NoError(t, err, "inserting")

			for _, op := range ops {
				got, err := db.Get(op.Key)
				require.NoErrorf(t, err, "%T.Get(%q)", db, op.Key)
				// Cast as strings to improve debug messages.
				want := string(op.Value)
				assert.Equal(t, want, string(got), "Recover nth batch-inserted value")
			}

			hash, err := db.Root()
			assert.NoError(t, err, "%T.Root()", db)
			assert.Lenf(t, hash, 32, "%T.Root()", db)
			// we know the hash starts with 0xf8
			assert.Equalf(t, byte(0xf8), hash[0], "First byte of %T.Root()", db)
			assert.Equalf(t, rootFromInsert, hash, "%T.Root() matches value returned by insertion", db)
		})
	}
}

func TestRangeDelete(t *testing.T) {
	db := newTestDatabase(t)
	ops := make([]KeyValue, 100)
	for i := range ops {
		ops[i] = kvForTest(i)
	}
	db.Batch(ops)

	const deletePrefix = 1
	db.Batch([]KeyValue{{
		Key: keyForTest(deletePrefix),
		// delete all keys that start with "key1"
		Value: nil,
	}})

	for _, op := range ops {
		got, err := db.Get(op.Key)
		require.NoError(t, err)

		if deleted := bytes.HasPrefix(op.Key, keyForTest(deletePrefix)); deleted {
			assert.Empty(t, err, got)
		} else {
			assert.Equal(t, op.Value, got)
		}
	}
}

func TestInvariants(t *testing.T) {
	db := newTestDatabase(t)
	hash, err := db.Root()
	require.NoError(t, err, "%T.Root()", db)
	assert.Equalf(t, make([]byte, 32), hash, "%T.Root() of empty trie")

	got, err := db.Get([]byte("non-existent"))
	require.NoError(t, err)
	assert.Emptyf(t, got, "%T.Get([non-existent key])", db)
}
