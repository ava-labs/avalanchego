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

func TestParallelProposals(t *testing.T) {
	db := newTestDatabase(t)

	// Create 10 proposals, each with 10 keys.
	const numProposals = 10
	const numKeys = 10
	proposals := make([]*Proposal, numProposals)
	for i := range proposals {
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

	// Check that each value is present in each proposal.
	for i, p := range proposals {
		for j := 0; j < numKeys; j++ {
			got, err := p.Get(keyForTest(i*numKeys + j))
			require.NoError(t, err, "Get(%d)", i*numKeys+j)
			assert.Equal(t, valForTest(i*numKeys+j), got, "Get(%d)", i*numKeys+j)
		}
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

	// Ensure we can still get values from the other proposals.
	for i := 1; i < numProposals; i++ {
		for j := 0; j < numKeys; j++ {
			got, err := proposals[i].Get(keyForTest(i*numKeys + j))
			require.NoError(t, err, "Get(%d)", i*numKeys+j)
			assert.Equal(t, valForTest(i*numKeys+j), got, "Get(%d)", i*numKeys+j)
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

	// Because they're invalid, we should not be able to get values from them.
	for i := 1; i < numProposals; i++ {
		for j := 0; j < numKeys; j++ {
			got, err := proposals[i].Get(keyForTest(i*numKeys + j))
			require.ErrorIs(t, err, errDroppedProposal, "Get(%d)", i*numKeys+j)
			assert.Empty(t, got, "Get(%d)", i*numKeys+j)
		}
	}
}

// Tests that a proposal that deletes all keys can be committed.
func TestDeleteAll(t *testing.T) {
	db := newTestDatabase(t)

	keys := make([][]byte, 10)
	vals := make([][]byte, 10)
	for i := range keys {
		keys[i] = keyForTest(i)
		vals[i] = valForTest(i)
	}
	// Insert 10 key-value pairs.
	db.Update(keys, vals)

	// Create a proposal that deletes all keys.
	proposal, err := db.Propose([][]byte{[]byte("key")}, [][]byte{nil})
	require.NoError(t, err, "Propose")

	// Check that the proposal doesn't have the keys we just inserted.
	for i := range keys {
		got, err := proposal.Get(keys[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Empty(t, got, "Get(%d)", i)
	}

	// Commit the proposal.
	err = proposal.Commit()
	require.NoError(t, err, "Commit")

	// Check that the database is empty.
	hash, err := db.Root()
	require.NoError(t, err, "%T.Root()", db)
	assert.Equalf(t, make([]byte, 32), hash, "%T.Root() of empty trie")
}

// Tests that a proposal with an invalid ID cannot be committed.
func TestCommitFakeProposal(t *testing.T) {
	db := newTestDatabase(t)

	// Create a fake proposal with an invalid ID.
	proposal := &Proposal{
		handle: db.handle,
		id:     1, // note that ID 0 is reserved for invalid proposals
	}

	// Attempt to get a value from the fake proposal.
	_, err := proposal.Get([]byte("non-existent"))
	require.Contains(t, err.Error(), "proposal not found", "Get(fake proposal)")
}

func TestDropProposal(t *testing.T) {
	db := newTestDatabase(t)

	// Create a proposal with 10 keys.
	keys := make([][]byte, 10)
	vals := make([][]byte, 10)
	for i := range keys {
		keys[i] = keyForTest(i)
		vals[i] = valForTest(i)
	}
	proposal, err := db.Propose(keys, vals)
	require.NoError(t, err, "Propose")

	// Drop the proposal.
	err = proposal.Drop()
	require.NoError(t, err, "Drop")

	// Attempt to commit the dropped proposal.
	err = proposal.Commit()
	require.ErrorIs(t, err, errDroppedProposal, "Commit(dropped proposal)")

	// Attempt to get a value from the dropped proposal.
	_, err = proposal.Get([]byte("non-existent"))
	require.ErrorIs(t, err, errDroppedProposal, "Get(dropped proposal)")

	// Attempt to "emulate" the proposal to ensure it isn't internally available still.
	proposal = &Proposal{
		handle: db.handle,
		id:     1,
	}
	_, err = proposal.Get([]byte("non-existent"))
	require.Contains(t, err.Error(), "proposal not found", "Get(fake proposal)")

	// Attempt to create a new proposal from the fake proposal.
	_, err = proposal.Propose([][]byte{[]byte("key")}, [][]byte{[]byte("value")})
	require.Contains(t, err.Error(), "proposal not found", "Propose(fake proposal)")

	// Attempt to commit the fake proposal.
	err = proposal.Commit()
	require.Contains(t, err.Error(), "proposal not found", "Commit(fake proposal)")
}

// Create a proposal with 10 key-value pairs.
// Tests that a proposal can be created from another proposal, and both can be
// committed sequentially.
func TestProposeFromProposal(t *testing.T) {
	db := newTestDatabase(t)

	// Create two sets of keys and values.
	keys1 := make([][]byte, 10)
	vals1 := make([][]byte, 10)
	keys2 := make([][]byte, 10)
	vals2 := make([][]byte, 10)
	for i := range keys1 {
		keys1[i] = keyForTest(i)
		vals1[i] = valForTest(i)
	}
	for i := range keys2 {
		keys2[i] = keyForTest(i + 10)
		vals2[i] = valForTest(i + 10)
	}

	// Create the first proposal.
	proposal1, err := db.Propose(keys1, vals1)
	require.NoError(t, err, "Propose")
	// Create the second proposal from the first.
	proposal2, err := proposal1.Propose(keys2, vals2)
	require.NoError(t, err, "Propose")

	// Assert that the first proposal doesn't have keys from the second.
	for i := range keys2 {
		got, err := proposal1.Get(keys2[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Empty(t, got, "Get(%d)", i)
	}
	// Assert that the second proposal has keys from the first.
	for i := range keys1 {
		got, err := proposal2.Get(keys1[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Equal(t, vals1[i], got, "Get(%d)", i)
	}

	// Commit the first proposal.
	err = proposal1.Commit()
	require.NoError(t, err, "Commit")

	// Assert that the second proposal has keys from the first and second.
	for i := range keys1 {
		got, err := db.Get(keys1[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Equal(t, vals1[i], got, "Get(%d)", i)
	}
	for i := range keys2 {
		got, err := proposal2.Get(keys2[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Equal(t, vals2[i], got, "Get(%d)", i)
	}

	// Commit the second proposal.
	err = proposal2.Commit()
	require.NoError(t, err, "Commit")

	// Assert that the database has keys from both proposals.
	for i := range keys1 {
		got, err := db.Get(keys1[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Equal(t, vals1[i], got, "Get(%d)", i)
	}
	for i := range keys2 {
		got, err := db.Get(keys2[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Equal(t, vals2[i], got, "Get(%d)", i)
	}
}

func TestDeepPropose(t *testing.T) {
	db := newTestDatabase(t)

	// Create a chain of two proposals, each with 10 keys.
	const numKeys = 10
	const numProposals = 10
	proposals := make([]*Proposal, numProposals)
	keys := make([][]byte, numKeys*numProposals)
	vals := make([][]byte, numKeys*numProposals)
	for i := range keys {
		keys[i] = keyForTest(i)
		vals[i] = valForTest(i)
	}

	for i := range proposals {
		var (
			p   *Proposal
			err error
		)
		if i == 0 {
			p, err = db.Propose(keys[i:(i+1)*numKeys], vals[i:(i+1)*numKeys])
			require.NoError(t, err, "Propose(%d)", i)
		} else {
			p, err = proposals[i-1].Propose(keys[i:(i+1)*numKeys], vals[i:(i+1)*numKeys])
			require.NoError(t, err, "Propose(%d)", i)
		}
		proposals[i] = p
	}

	// Check that each value is present in the final proposal.
	for i := range keys {
		got, err := proposals[numProposals-1].Get(keys[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Equal(t, vals[i], got, "Get(%d)", i)
	}

	// Commit each proposal sequentially, and ensure that the values are
	// present in the database after each commit.
	for i := range proposals {
		err := proposals[i].Commit()
		require.NoError(t, err, "Commit(%d)", i)

		for j := i * numKeys; j < (i+1)*numKeys; j++ {
			got, err := db.Get(keys[j])
			require.NoError(t, err, "Get(%d)", j)
			assert.Equal(t, vals[j], got, "Get(%d)", j)
		}
	}
}

// Tests that dropping a proposal and committing another one still allows
// access to the data of children proposals
func TestDropProposalAndCommit(t *testing.T) {
	db := newTestDatabase(t)

	// Create a chain of three proposals, each with 10 keys.
	const numKeys = 10
	const numProposals = 3
	proposals := make([]*Proposal, numProposals)
	keys := make([][]byte, numKeys*numProposals)
	vals := make([][]byte, numKeys*numProposals)
	for i := range keys {
		keys[i] = keyForTest(i)
		vals[i] = valForTest(i)
	}
	for i := range proposals {
		var (
			p   *Proposal
			err error
		)
		if i == 0 {
			p, err = db.Propose(keys[i:(i+1)*numKeys], vals[i:(i+1)*numKeys])
			require.NoError(t, err, "Propose(%d)", i)
		} else {
			p, err = proposals[i-1].Propose(keys[i:(i+1)*numKeys], vals[i:(i+1)*numKeys])
			require.NoError(t, err, "Propose(%d)", i)
		}
		proposals[i] = p
	}

	// drop the second proposal
	err := proposals[1].Drop()
	require.NoError(t, err, "Drop(%d)", 1)
	// Commit the first proposal
	err = proposals[0].Commit()
	require.NoError(t, err, "Commit(%d)", 0)

	// Check that the second proposal is dropped
	_, err = proposals[1].Get(keys[0])
	require.ErrorIs(t, err, errDroppedProposal, "Get(%d)", 0)

	// Check that all keys can be accessed from the final proposal
	for i := range keys {
		got, err := proposals[numProposals-1].Get(keys[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Equal(t, vals[i], got, "Get(%d)", i)
	}
}

// Create two proposals with the same root, and ensure that these proposals
// are identified as unique in the backend.
/*
 /- P1 -\  /- P4
R1       P2
 \- P2 -/  \- P5
*/
func TestProposeSameRoot(t *testing.T) {
	db := newTestDatabase(t)

	// Create two chains of proposals, resulting in the same root.
	keys := make([][]byte, 10)
	vals := make([][]byte, 10)
	for i := range keys {
		keys[i] = keyForTest(i)
		vals[i] = valForTest(i)
	}

	// Create the first proposal chain.
	proposal1, err := db.Propose(keys[0:5], vals[0:5])
	require.NoError(t, err, "Propose")
	proposal3_top, err := proposal1.Propose(keys[5:10], vals[5:10])
	require.NoError(t, err, "Propose")
	// Create the second proposal chain.
	proposal2, err := db.Propose(keys[5:10], vals[5:10])
	require.NoError(t, err, "Propose")
	proposal3_bottom, err := proposal2.Propose(keys[0:5], vals[0:5])
	require.NoError(t, err, "Propose")
	// Because the proposals are identical, they should have the same root.

	// Create a unique proposal from each of the two chains.
	top_keys := make([][]byte, 5)
	top_vals := make([][]byte, 5)
	for i := range top_keys {
		top_keys[i] = keyForTest(i + 10)
		top_vals[i] = valForTest(i + 10)
	}
	bot_keys := make([][]byte, 5)
	bot_vals := make([][]byte, 5)
	for i := range bot_keys {
		bot_keys[i] = keyForTest(i + 20)
		bot_vals[i] = valForTest(i + 20)
	}
	proposal4, err := proposal3_top.Propose(top_keys, top_vals)
	require.NoError(t, err, "Propose")
	proposal5, err := proposal3_bottom.Propose(bot_keys, bot_vals)
	require.NoError(t, err, "Propose")

	// Now we will commit the top chain, and check that the bottom chain is still valid.
	err = proposal1.Commit()
	require.NoError(t, err, "Commit")
	err = proposal3_top.Commit()
	require.NoError(t, err, "Commit")

	// Check that both final proposals are valid.
	for i := range keys {
		got, err := proposal4.Get(keys[i])
		require.NoError(t, err, "P4 Get(%d)", i)
		assert.Equal(t, vals[i], got, "P4 Get(%d)", i)
		got, err = proposal5.Get(keys[i])
		require.NoError(t, err, "P5 Get(%d)", i)
		assert.Equal(t, vals[i], got, "P5 Get(%d)", i)
	}

	// Attempt to commit P5. Since this isn't in the canonical chain, it should
	// fail.
	err = proposal5.Commit()
	require.Error(t, err, "Commit P5") // this error is internal to firewood

	// We should be able to commit P4, since it is in the canonical chain.
	err = proposal4.Commit()
	require.NoError(t, err, "Commit P4")
}

// Tests that an empty revision can be retrieved.
func TestRevision(t *testing.T) {
	db := newTestDatabase(t)

	keys := make([][]byte, 10)
	vals := make([][]byte, 10)
	for i := range keys {
		keys[i] = keyForTest(i)
		vals[i] = valForTest(i)
	}

	// Create a proposal with 10 key-value pairs.
	proposal, err := db.Propose(keys, vals)
	require.NoError(t, err, "Propose")

	// Commit the proposal.
	err = proposal.Commit()
	require.NoError(t, err, "Commit")

	root, err := db.Root()
	require.NoError(t, err, "%T.Root()", db)

	// Create a revision from this root.
	revision, err := db.Revision(root)
	require.NoError(t, err, "Revision")
	// Check that all keys can be retrieved from the revision.
	for i := range keys {
		got, err := revision.Get(keys[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Equal(t, valForTest(i), got, "Get(%d)", i)
	}

	// Create a second proposal with 10 key-value pairs.
	keys2 := make([][]byte, 10)
	vals2 := make([][]byte, 10)
	for i := range keys2 {
		keys2[i] = keyForTest(i + 10)
		vals2[i] = valForTest(i + 10)
	}
	proposal2, err := db.Propose(keys2, vals2)
	require.NoError(t, err, "Propose")
	// Commit the proposal.
	err = proposal2.Commit()
	require.NoError(t, err, "Commit")

	// Create a "new" revision from the first old root.
	revision, err = db.Revision(root)
	require.NoError(t, err, "Revision")
	// Check that all keys can be retrieved from the revision.
	for i := range keys {
		got, err := revision.Get(keys[i])
		require.NoError(t, err, "Get(%d)", i)
		assert.Equal(t, valForTest(i), got, "Get(%d)", i)
	}
}

func TestFakeRevision(t *testing.T) {
	db := newTestDatabase(t)

	// Create a nil revision.
	_, err := db.Revision(nil)
	require.ErrorIs(t, err, errInvalidRootLength, "Revision(nil)")

	// Create a fake revision with an invalid root.
	invalidRoot := []byte("not a valid root")
	_, err = db.Revision(invalidRoot)
	require.ErrorIs(t, err, errInvalidRootLength, "Revision(invalid root)")

	// Create a fake revision with an valid root.
	validRoot := []byte("counting 32 bytes to make a hash")
	assert.Len(t, validRoot, 32, "valid root")
	_, err = db.Revision(validRoot)
	require.ErrorIs(t, err, errRevisionNotFound, "Revision(valid root)")
}
