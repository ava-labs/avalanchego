package firewood

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	ethhashKey        = "ethhash"
	firewoodKey       = "firewood"
	emptyKey          = "empty"
	insert100Key      = "100"
	emptyEthhashRoot  = "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	emptyFirewoodRoot = "0000000000000000000000000000000000000000000000000000000000000000"
)

// expectedRoots contains the expected root hashes for different use cases across both default
// firewood hashing and ethhash.
// By default, TestMain infers which mode Firewood is operating in and selects the expected roots
// accordingly (this does turn test empty database into an effective no-op).
//
// To test a specific hashing mode explicitly, set the environment variable:
// TEST_FIREWOOD_HASH_MODE=ethhash or TEST_FIREWOOD_HASH_MODE=firewood
// This will skip the inference step and enforce we use the expected roots for the specified mode.
var (
	// expectedRoots contains a mapping of expected root hashes for different test
	// vectors.
	expectedRootModes = map[string]map[string]string{
		ethhashKey: {
			emptyKey:     emptyEthhashRoot,
			insert100Key: "c25a0076e0337d7c982c3c9dfa445c8088242a0a607f9d9def3762765bcb0fde",
		},
		firewoodKey: {
			emptyKey:     emptyFirewoodRoot,
			insert100Key: "f858b51ada79c4abeb6566ef1204a453030dba1cca3526d174e2cb3ce2aadc57",
		},
	}
	expectedEmptyRootToMode = map[string]string{
		emptyEthhashRoot:  ethhashKey,
		emptyFirewoodRoot: firewoodKey,
	}
	expectedRoots map[string]string
)

func inferHashingMode() (string, error) {
	dbFile := filepath.Join(os.TempDir(), "test.db")
	db, closeDB, err := newDatabase(dbFile)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = closeDB()
		_ = os.Remove(dbFile)
	}()

	actualEmptyRoot, err := db.Root()
	if err != nil {
		return "", fmt.Errorf("failed to get root of empty database: %w", err)
	}
	actualEmptyRootHex := hex.EncodeToString(actualEmptyRoot)

	actualFwMode, ok := expectedEmptyRootToMode[actualEmptyRootHex]
	if !ok {
		return "", fmt.Errorf("unknown empty root %q, cannot infer mode", actualEmptyRootHex)
	}

	return actualFwMode, nil
}

func TestMain(m *testing.M) {
	// The cgocheck debugging flag checks that all pointers are pinned.
	// TODO(arr4n) why doesn't `//go:debug cgocheck=1` work? https://go.dev/doc/godebug
	debug := strings.Split(os.Getenv("GODEBUG"), ",")
	var hasCgoCheck bool
	for _, kv := range debug {
		switch strings.TrimSpace(kv) {
		case "cgocheck=1":
			hasCgoCheck = true
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

	// If TEST_FIREWOOD_HASH_MODE is set, use it to select the expected roots.
	// Otherwise, infer the hash mode from an empty database.
	hashMode := os.Getenv("TEST_FIREWOOD_HASH_MODE")
	if hashMode == "" {
		inferredHashMode, err := inferHashingMode()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to infer hash mode %v\n", err)
			os.Exit(1)
		}
		hashMode = inferredHashMode
	}
	selectedExpectedRoots, ok := expectedRootModes[hashMode]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown hash mode %q\n", hashMode)
		os.Exit(1)
	}
	expectedRoots = selectedExpectedRoots

	os.Exit(m.Run())
}

func newTestDatabase(t *testing.T) *Database {
	t.Helper()

	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, closeDB, err := newDatabase(dbFile)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, closeDB())
	})
	return db
}

func newDatabase(dbFile string) (*Database, func() error, error) {
	conf := DefaultConfig()
	conf.MetricsPort = 0
	conf.Create = true

	f, err := New(dbFile, conf)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create new database at filepath %q: %w", dbFile, err)
	}
	return f, f.Close, nil
}

// Tests that a single key-value pair can be inserted and retrieved.
// This doesn't require storing a proposal across the FFI boundary.
func TestInsert(t *testing.T) {
	db := newTestDatabase(t)
	const (
		key = "abc"
		val = "def"
	)

	_, err := db.Batch([]KeyValue{
		{[]byte(key), []byte(val)},
	})
	require.NoError(t, err, "Batch(%q)", key)

	got, err := db.Get([]byte(key))
	require.NoErrorf(t, err, "%T.Get(%q)", db, key)
	require.Equal(t, val, string(got), "Recover lone batch-inserted value")
}

func TestClosedDatabase(t *testing.T) {
	r := require.New(t)
	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, _, err := newDatabase(dbFile)
	r.NoError(err)

	r.NoError(db.Close())

	_, err = db.Root()
	r.ErrorIs(err, errDBClosed)

	root, err := db.Update(
		[][]byte{[]byte("key")},
		[][]byte{[]byte("value")},
	)
	r.Empty(root)
	r.ErrorIs(err, errDBClosed)

	err = db.Close()
	r.ErrorIs(err, errDBClosed)
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
	type dbView interface {
		Get(key []byte) ([]byte, error)
		Propose(keys, vals [][]byte) (*Proposal, error)
		Root() ([]byte, error)
	}

	tests := []struct {
		name   string
		insert func(dbView, [][]byte, [][]byte) (dbView, error)
	}{
		{
			name: "Propose and Commit",
			insert: func(db dbView, keys, vals [][]byte) (dbView, error) {
				proposal, err := db.Propose(keys, vals)
				if err != nil {
					return nil, err
				}
				err = proposal.Commit()
				if err != nil {
					return nil, err
				}
				return db, nil
			},
		},
		{
			name: "Update",
			insert: func(db dbView, keys, vals [][]byte) (dbView, error) {
				actualDB, ok := db.(*Database)
				if !ok {
					return nil, fmt.Errorf("expected *Database, got %T", db)
				}
				_, err := actualDB.Update(keys, vals)
				return db, err
			},
		},
		{
			name: "Propose",
			insert: func(db dbView, keys, vals [][]byte) (dbView, error) {
				proposal, err := db.Propose(keys, vals)
				if err != nil {
					return nil, err
				}
				return proposal, nil
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
			newDB, err := tt.insert(db, keys, vals)
			require.NoError(t, err, "inserting")

			for i := range keys {
				got, err := newDB.Get(keys[i])
				require.NoErrorf(t, err, "%T.Get(%q)", db, keys[i])
				// Cast as strings to improve debug messages.
				want := string(vals[i])
				require.Equal(t, want, string(got), "Recover nth batch-inserted value")
			}

			hash, err := newDB.Root()
			require.NoError(t, err, "%T.Root()", db)

			rootFromInsert, err := newDB.Root()
			require.NoError(t, err, "%T.Root() after insertion", db)

			// Assert the hash is exactly as expected. Test failure indicates a
			// non-hash compatible change has been made since the string was set.
			// If that's expected, update the string at the top of the file to
			// fix this test.
			expectedHashHex := expectedRoots[insert100Key]
			expectedHash, err := hex.DecodeString(expectedHashHex)
			require.NoError(t, err, "failed to decode expected hash")
			require.Equal(t, expectedHash, hash, "Root hash mismatch.\nExpected (hex): %x\nActual (hex): %x", expectedHash, hash)
			require.Equalf(t, rootFromInsert, hash, "%T.Root() matches value returned by insertion", db)
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
	_, err := db.Batch(ops)
	require.NoError(t, err, "Batch")

	const deletePrefix = 1
	_, err = db.Batch([]KeyValue{{
		Key: keyForTest(deletePrefix),
		// delete all keys that start with "key1"
		Value: nil,
	}})
	require.NoError(t, err, "Batch")

	for _, op := range ops {
		got, err := db.Get(op.Key)
		require.NoError(t, err)

		if deleted := bytes.HasPrefix(op.Key, keyForTest(deletePrefix)); deleted {
			require.NoError(t, err, got)
		} else {
			require.Equal(t, op.Value, got)
		}
	}
}

// Tests that the database is empty after creation and doesn't panic.
func TestInvariants(t *testing.T) {
	db := newTestDatabase(t)
	hash, err := db.Root()
	require.NoError(t, err, "%T.Root()", db)

	emptyRootStr := expectedRoots[emptyKey]
	expectedHash, err := hex.DecodeString(emptyRootStr)
	require.NoError(t, err)
	require.Equalf(t, expectedHash, hash, "expected %x, got %x", expectedHash, hash)

	got, err := db.Get([]byte("non-existent"))
	require.NoError(t, err)
	require.Emptyf(t, got, "%T.Get([non-existent key])", db)
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
			require.Equal(t, valForTest(i*numKeys+j), got, "Get(%d)", i*numKeys+j)
		}
	}

	// Commit only the first proposal.
	err := proposals[0].Commit()
	require.NoError(t, err, "Commit(%d)", 0)
	// Check that the first proposal's keys are present.
	for j := 0; j < numKeys; j++ {
		got, err := db.Get(keyForTest(j))
		require.NoError(t, err, "Get(%d)", j)
		require.Equal(t, valForTest(j), got, "Get(%d)", j)
	}
	// Check that the other proposals' keys are not present.
	for i := 1; i < numProposals; i++ {
		for j := 0; j < numKeys; j++ {
			got, err := db.Get(keyForTest(i*numKeys + j))
			require.NoError(t, err, "Get(%d)", i*numKeys+j)
			require.Empty(t, got, "Get(%d)", i*numKeys+j)
		}
	}

	// Ensure we can still get values from the other proposals.
	for i := 1; i < numProposals; i++ {
		for j := 0; j < numKeys; j++ {
			got, err := proposals[i].Get(keyForTest(i*numKeys + j))
			require.NoError(t, err, "Get(%d)", i*numKeys+j)
			require.Equal(t, valForTest(i*numKeys+j), got, "Get(%d)", i*numKeys+j)
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
			require.Empty(t, got, "Get(%d)", i*numKeys+j)
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
	_, err := db.Update(keys, vals)
	require.NoError(t, err, "Update")

	// Create a proposal that deletes all keys.
	proposal, err := db.Propose([][]byte{[]byte("key")}, [][]byte{nil})
	require.NoError(t, err, "Propose")

	// Check that the proposal doesn't have the keys we just inserted.
	for i := range keys {
		got, err := proposal.Get(keys[i])
		require.NoError(t, err, "Get(%d)", i)
		require.Empty(t, got, "Get(%d)", i)
	}

	emptyRootStr := expectedRoots[emptyKey]
	expectedHash, err := hex.DecodeString(emptyRootStr)
	require.NoError(t, err, "Decode expected empty root hash")

	hash, err := proposal.Root()
	require.NoError(t, err, "%T.Root() after commit", proposal)
	require.Equalf(t, expectedHash, hash, "%T.Root() of empty trie", db)

	// Commit the proposal.
	err = proposal.Commit()
	require.NoError(t, err, "Commit")

	// Check that the database is empty.
	hash, err = db.Root()
	require.NoError(t, err, "%T.Root()", db)
	require.Equalf(t, expectedHash, hash, "%T.Root() of empty trie", db)
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

	// Check all operations on the dropped proposal.
	err = proposal.Commit()
	require.ErrorIs(t, err, errDroppedProposal, "Commit(dropped proposal)")
	_, err = proposal.Get([]byte("non-existent"))
	require.ErrorIs(t, err, errDroppedProposal, "Get(dropped proposal)")
	_, err = proposal.Root()
	require.ErrorIs(t, err, errDroppedProposal, "Root(dropped proposal)")

	// Attempt to "emulate" the proposal to ensure it isn't internally available still.
	proposal = &Proposal{
		handle: db.handle,
		id:     1,
	}

	// Check all operations on the fake proposal.
	_, err = proposal.Get([]byte("non-existent"))
	require.Contains(t, err.Error(), "proposal not found", "Get(fake proposal)")
	_, err = proposal.Propose([][]byte{[]byte("key")}, [][]byte{[]byte("value")})
	require.Contains(t, err.Error(), "proposal not found", "Propose(fake proposal)")
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
		require.Empty(t, got, "Get(%d)", i)
	}
	// Assert that the second proposal has keys from the first.
	for i := range keys1 {
		got, err := proposal2.Get(keys1[i])
		require.NoError(t, err, "Get(%d)", i)
		require.Equal(t, vals1[i], got, "Get(%d)", i)
	}

	// Commit the first proposal.
	err = proposal1.Commit()
	require.NoError(t, err, "Commit")

	// Assert that the second proposal has keys from the first and second.
	for i := range keys1 {
		got, err := db.Get(keys1[i])
		require.NoError(t, err, "Get(%d)", i)
		require.Equal(t, vals1[i], got, "Get(%d)", i)
	}
	for i := range keys2 {
		got, err := proposal2.Get(keys2[i])
		require.NoError(t, err, "Get(%d)", i)
		require.Equal(t, vals2[i], got, "Get(%d)", i)
	}

	// Commit the second proposal.
	err = proposal2.Commit()
	require.NoError(t, err, "Commit")

	// Assert that the database has keys from both proposals.
	for i := range keys1 {
		got, err := db.Get(keys1[i])
		require.NoError(t, err, "Get(%d)", i)
		require.Equal(t, vals1[i], got, "Get(%d)", i)
	}
	for i := range keys2 {
		got, err := db.Get(keys2[i])
		require.NoError(t, err, "Get(%d)", i)
		require.Equal(t, vals2[i], got, "Get(%d)", i)
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
		require.Equal(t, vals[i], got, "Get(%d)", i)
	}

	// Commit each proposal sequentially, and ensure that the values are
	// present in the database after each commit.
	for i := range proposals {
		err := proposals[i].Commit()
		require.NoError(t, err, "Commit(%d)", i)

		for j := i * numKeys; j < (i+1)*numKeys; j++ {
			got, err := db.Get(keys[j])
			require.NoError(t, err, "Get(%d)", j)
			require.Equal(t, vals[j], got, "Get(%d)", j)
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
		require.Equal(t, vals[i], got, "Get(%d)", i)
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
	proposal3Top, err := proposal1.Propose(keys[5:10], vals[5:10])
	require.NoError(t, err, "Propose")
	// Create the second proposal chain.
	proposal2, err := db.Propose(keys[5:10], vals[5:10])
	require.NoError(t, err, "Propose")
	proposal3Bottom, err := proposal2.Propose(keys[0:5], vals[0:5])
	require.NoError(t, err, "Propose")
	// Because the proposals are identical, they should have the same root.

	// Create a unique proposal from each of the two chains.
	topKeys := make([][]byte, 5)
	topVals := make([][]byte, 5)
	for i := range topKeys {
		topKeys[i] = keyForTest(i + 10)
		topVals[i] = valForTest(i + 10)
	}
	bottomKeys := make([][]byte, 5)
	bottomVals := make([][]byte, 5)
	for i := range bottomKeys {
		bottomKeys[i] = keyForTest(i + 20)
		bottomVals[i] = valForTest(i + 20)
	}
	proposal4, err := proposal3Top.Propose(topKeys, topVals)
	require.NoError(t, err, "Propose")
	proposal5, err := proposal3Bottom.Propose(bottomKeys, bottomVals)
	require.NoError(t, err, "Propose")

	// Now we will commit the top chain, and check that the bottom chain is still valid.
	err = proposal1.Commit()
	require.NoError(t, err, "Commit")
	err = proposal3Top.Commit()
	require.NoError(t, err, "Commit")

	// Check that both final proposals are valid.
	for i := range keys {
		got, err := proposal4.Get(keys[i])
		require.NoError(t, err, "P4 Get(%d)", i)
		require.Equal(t, vals[i], got, "P4 Get(%d)", i)
		got, err = proposal5.Get(keys[i])
		require.NoError(t, err, "P5 Get(%d)", i)
		require.Equal(t, vals[i], got, "P5 Get(%d)", i)
	}

	// Attempt to commit P5. Since this isn't in the canonical chain, it should
	// fail.
	err = proposal5.Commit()
	require.Contains(t, err.Error(), "commit the parents of this proposal first", "Commit P5") // this error is internal to firewood

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
		require.Equal(t, valForTest(i), got, "Get(%d)", i)
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
		require.Equal(t, valForTest(i), got, "Get(%d)", i)
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
	require.Len(t, validRoot, 32, "valid root")
	_, err = db.Revision(validRoot)
	require.ErrorIs(t, err, errRevisionNotFound, "Revision(valid root)")
}

// Tests that edge case `Get` calls are handled correctly.
func TestGetNilCases(t *testing.T) {
	db := newTestDatabase(t)

	// Commit 10 key-value pairs.
	keys := make([][]byte, 20)
	vals := make([][]byte, 20)
	for i := range keys {
		keys[i] = keyForTest(i)
		vals[i] = valForTest(i)
	}
	root, err := db.Update(keys[:10], vals[:10])
	require.NoError(t, err, "Update")

	// Create the other views
	proposal, err := db.Propose(keys[10:], vals[10:])
	require.NoError(t, err, "Propose")
	revision, err := db.Revision(root)
	require.NoError(t, err, "Revision")

	// Create edge case keys.
	specialKeys := [][]byte{
		nil,
		{}, // empty slice
	}
	for _, k := range specialKeys {
		got, err := db.Get(k)
		require.NoError(t, err, "db.Get(%q)", k)
		require.Empty(t, got, "db.Get(%q)", k)

		got, err = revision.Get(k)
		require.NoError(t, err, "Revision.Get(%q)", k)
		require.Empty(t, got, "Revision.Get(%q)", k)

		got, err = proposal.Get(k)
		require.NoError(t, err, "Proposal.Get(%q)", k)
		require.Empty(t, got, "Proposal.Get(%q)", k)
	}
}
