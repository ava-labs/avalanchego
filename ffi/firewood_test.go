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
	conf.MetricsPort = 0
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

// Tests that a single key-value pair can be inserted and retrieved.
// This doesn't require storing a proposal across the FFI boundary.
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

// Attempt to make a call to a nil or invalid handle.
// Each function should return an error and not panic.
func TestGetBadHandle(t *testing.T) {
	db := &Database{handle: nil}

	// This ignores error, but still shouldn't panic.
	_, err := db.Get([]byte("non-existent"))
	assert.ErrorIs(t, err, errDbClosed)

	// We ignore the error, but it shouldn't panic.
	_, err = db.Root()
	assert.ErrorIs(t, err, errDbClosed)

	root, err := db.Update(
		[][]byte{[]byte("key")},
		[][]byte{[]byte("value")},
	)
	assert.Empty(t, root)
	assert.ErrorIs(t, err, errDbClosed)

	err = db.Close()
	require.ErrorIs(t, err, errDbClosed)
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

// Tests that 100 key-value pairs can be inserted and retrieved.
// This happens in two ways:
// 1. By calling [Database.Propose] and then [Proposal.Commit].
// 2. By calling [Database.Update] directly - no proposal storage is needed.
func TestInsert100(t *testing.T) {
	tests := []struct {
		name   string
		insert func(*Database, [][]byte, [][]byte) (root []byte, _ error)
	}{
		{
			name: "Propose",
			insert: func(db *Database, keys, vals [][]byte) ([]byte, error) {
				proposal, err := db.Propose(keys, vals)
				if err != nil {
					return nil, err
				}
				err = proposal.Commit()
				if err != nil {
					return nil, err
				}
				return db.Root()
			},
		},
		{
			name: "Update",
			insert: func(db *Database, keys, vals [][]byte) ([]byte, error) {
				return db.Update(keys, vals)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDatabase(t)

			keys := make([][]byte, 100)
			vals := make([][]byte, 100)
			for i := range keys {
				keys[i] = keyForTest(i)
				vals[i] = valForTest(i)
			}
			rootFromInsert, err := tt.insert(db, keys, vals)
			require.NoError(t, err, "inserting")

			for i := range keys {
				got, err := db.Get(keys[i])
				require.NoErrorf(t, err, "%T.Get(%q)", db, keys[i])
				// Cast as strings to improve debug messages.
				want := string(vals[i])
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

// Tests that a range of keys can be deleted.
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

// Tests that the database is empty after creation and doesn't panic.
func TestInvariants(t *testing.T) {
	db := newTestDatabase(t)
	hash, err := db.Root()
	require.NoError(t, err, "%T.Root()", db)
	assert.Equalf(t, make([]byte, 32), hash, "%T.Root() of empty trie")

	got, err := db.Get([]byte("non-existent"))
	require.NoError(t, err)
	assert.Emptyf(t, got, "%T.Get([non-existent key])", db)
}

func TestMultipleProposals(t *testing.T) {
	db := newTestDatabase(t)

	// Create 10 proposals, each with 10 keys.
	const numProposals = 10
	const numKeys = 10
	proposals := make([]*Proposal, numProposals)
	for i := 0; i < numProposals; i++ {
		keys := make([][]byte, numKeys)
		vals := make([][]byte, numKeys)
		for j := 0; j < numKeys; j++ {
			keys[j] = keyForTest(i*numKeys + j)
			vals[j] = valForTest(i*numKeys + j)
		}
		proposal, err := db.Propose(keys, vals)
		require.NoError(t, err, "Propose(%d)", i)
		proposals[i] = proposal
	}

	// Commit only the first proposal.
	err := proposals[0].Commit()
	require.NoError(t, err, "Commit(%d)", 0)
	// Check that the first proposal's keys are present.
	for j := 0; j < numKeys; j++ {
		got, err := db.Get(keyForTest(j))
		require.NoError(t, err, "Get(%d)", j)
		assert.Equal(t, valForTest(j), got, "Get(%d)", j)
	}
	// Check that the other proposals' keys are not present.
	for i := 1; i < numProposals; i++ {
		for j := 0; j < numKeys; j++ {
			got, err := db.Get(keyForTest(i*numKeys + j))
			require.NoError(t, err, "Get(%d)", i*numKeys+j)
			assert.Empty(t, got, "Get(%d)", i*numKeys+j)
		}
	}

	// Now we ensure we cannot commit the other proposals.
	for i := 1; i < numProposals; i++ {
		err := proposals[i].Commit()
		require.Contains(t, err.Error(), "commit the parents of this proposal first", "Commit(%d)", i)
	}

	// After attempting to commit the other proposals, they should be completely invalid.
	for i := 1; i < numProposals; i++ {
		err := proposals[i].Commit()
		require.ErrorIs(t, err, errDroppedProposal, "Commit(%d)", i)
	}
}

// Tests that a proposal with an invalid ID cannot be committed.
func TestFakeProposal(t *testing.T) {
	db := newTestDatabase(t)

	// Create a fake proposal with an invalid ID.
	proposal := &Proposal{
		handle: db.handle,
		id:     1, // note that ID 0 is reserved for invalid proposals
	}

	// Attempt to commit the fake proposal.
	err := proposal.Commit()
	require.Contains(t, err.Error(), "proposal not found", "Commit(fake proposal)")
}
