// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

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
	r := require.New(t)

	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, closeDB, err := newDatabase(dbFile)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(closeDB())
	})
	return db
}

func newDatabase(dbFile string) (*Database, func() error, error) {
	conf := DefaultConfig()
	conf.Truncate = true // in tests, we use filepath.Join, which creates an empty file

	f, err := New(dbFile, conf)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create new database at filepath %q: %w", dbFile, err)
	}
	return f, f.Close, nil
}

func TestUpdateSingleKV(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals := kvForTest(1)
	_, err := db.Update(keys, vals)
	r.NoError(err)

	got, err := db.Get(keys[0])
	r.NoError(err)
	r.Equal(vals[0], got)
}

func TestUpdateMultiKV(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals := kvForTest(10)
	_, err := db.Update(keys, vals)
	r.NoError(err)

	for i, key := range keys {
		got, err := db.Get(key)
		r.NoError(err)
		r.Equal(vals[i], got)
	}
}

func TestTruncateDatabase(t *testing.T) {
	r := require.New(t)
	dbFile := filepath.Join(t.TempDir(), "test.db")
	// Create a new database with truncate enabled.
	config := DefaultConfig()
	config.Truncate = true
	db, err := New(dbFile, config)
	r.NoError(err)

	// Insert some data.
	keys, vals := kvForTest(10)
	_, err = db.Update(keys, vals)
	r.NoError(err)

	// Close the database.
	r.NoError(db.Close())

	// Reopen the database with truncate enabled.
	db, err = New(dbFile, config)
	r.NoError(err)

	// Check that the database is empty after truncation.
	hash, err := db.Root()
	r.NoError(err)
	emptyRootStr := expectedRoots[emptyKey]
	expectedHash, err := hex.DecodeString(emptyRootStr)
	r.NoError(err)
	r.Equal(expectedHash, hash, "Root hash mismatch after truncation")

	r.NoError(db.Close())
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

func kvForTest(num int) ([][]byte, [][]byte) {
	keys := make([][]byte, num)
	vals := make([][]byte, num)
	for i := range keys {
		keys[i] = keyForTest(i)
		vals[i] = valForTest(i)
	}
	return keys, vals
}

// Tests that 100 key-value pairs can be inserted and retrieved.
// This happens in three ways:
// 1. By calling [Database.Propose] and then [Proposal.Commit].
// 2. By calling [Database.Update] directly - no proposal storage is needed.
// 3. By calling [Database.Propose] and not committing, which returns a proposal.
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
		keys, vals := kvForTest(100)
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			db := newTestDatabase(t)

			newDB, err := tt.insert(db, keys, vals)
			r.NoError(err)

			for i := range keys {
				got, err := newDB.Get(keys[i])
				r.NoError(err)
				// Cast as strings to improve debug messages.
				want := string(vals[i])
				r.Equal(want, string(got))
			}

			hash, err := newDB.Root()
			r.NoError(err)

			rootFromInsert, err := newDB.Root()
			r.NoError(err)

			// Assert the hash is exactly as expected. Test failure indicates a
			// non-hash compatible change has been made since the string was set.
			// If that's expected, update the string at the top of the file to
			// fix this test.
			expectedHashHex := expectedRoots[insert100Key]
			expectedHash, err := hex.DecodeString(expectedHashHex)
			r.NoError(err)
			r.Equal(expectedHash, hash, "Root hash mismatch.\nExpected (hex): %x\nActual (hex): %x", expectedHash, hash)
			r.Equal(rootFromInsert, hash)
		})
	}
}

// Tests that a range of keys can be deleted.
func TestRangeDelete(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals := kvForTest(100)
	_, err := db.Update(keys, vals)
	r.NoError(err)

	const deletePrefix = 1
	_, err = db.Update([][]byte{keyForTest(deletePrefix)}, [][]byte{{}})
	r.NoError(err)

	for i := range keys {
		got, err := db.Get(keys[i])
		r.NoError(err)

		if deleted := bytes.HasPrefix(keys[i], keyForTest(deletePrefix)); deleted {
			r.NoError(err)
		} else {
			r.Equal(vals[i], got)
		}
	}
}

// Tests that the database is empty after creation and doesn't panic.
func TestInvariants(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	hash, err := db.Root()
	r.NoError(err)

	emptyRootStr := expectedRoots[emptyKey]
	expectedHash, err := hex.DecodeString(emptyRootStr)
	r.NoError(err)
	r.Equalf(expectedHash, hash, "expected %x, got %x", expectedHash, hash)

	got, err := db.Get([]byte("non-existent"))
	r.NoError(err)
	r.Empty(got)
}

func TestConflictingProposals(t *testing.T) {
	r := require.New(t)
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
		r.NoError(err)
		proposals[i] = proposal
	}

	// Check that each value is present in each proposal.
	for i, p := range proposals {
		for j := 0; j < numKeys; j++ {
			got, err := p.Get(keyForTest(i*numKeys + j))
			r.NoError(err)
			r.Equal(valForTest(i*numKeys+j), got, "Get(%d)", i*numKeys+j)
		}
	}

	// Commit only the first proposal.
	err := proposals[0].Commit()
	r.NoError(err)
	// Check that the first proposal's keys are present.
	for j := 0; j < numKeys; j++ {
		got, err := db.Get(keyForTest(j))
		r.NoError(err)
		r.Equal(valForTest(j), got, "Get(%d)", j)
	}
	// Check that the other proposals' keys are not present.
	for i := 1; i < numProposals; i++ {
		for j := 0; j < numKeys; j++ {
			got, err := db.Get(keyForTest(i*numKeys + j))
			r.Empty(got, "Get(%d)", i*numKeys+j)
			r.NoError(err, "Get(%d)", i*numKeys+j)
		}
	}

	// Ensure we can still get values from the other proposals.
	for i := 1; i < numProposals; i++ {
		for j := 0; j < numKeys; j++ {
			got, err := proposals[i].Get(keyForTest(i*numKeys + j))
			r.NoError(err, "Get(%d)", i*numKeys+j)
			r.Equal(valForTest(i*numKeys+j), got, "Get(%d)", i*numKeys+j)
		}
	}

	// Now we ensure we cannot commit the other proposals.
	for i := 1; i < numProposals; i++ {
		err := proposals[i].Commit()
		r.Contains(err.Error(), "commit the parents of this proposal first", "Commit(%d)", i)
	}

	// After attempting to commit the other proposals, they should be completely invalid.
	for i := 1; i < numProposals; i++ {
		err := proposals[i].Commit()
		r.ErrorIs(err, errDroppedProposal, "Commit(%d)", i)
	}

	// Because they're invalid, we should not be able to get values from them.
	for i := 1; i < numProposals; i++ {
		for j := 0; j < numKeys; j++ {
			got, err := proposals[i].Get(keyForTest(i*numKeys + j))
			r.ErrorIs(err, errDroppedProposal, "Get(%d)", i*numKeys+j)
			r.Empty(got, "Get(%d)", i*numKeys+j)
		}
	}
}

// Tests that a proposal that deletes all keys can be committed.
func TestDeleteAll(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals := kvForTest(10)
	// Insert 10 key-value pairs.
	_, err := db.Update(keys, vals)
	r.NoError(err)

	// Create a proposal that deletes all keys.
	proposal, err := db.Propose([][]byte{[]byte("key")}, [][]byte{nil})
	r.NoError(err)

	// Check that the proposal doesn't have the keys we just inserted.
	for i := range keys {
		got, err := proposal.Get(keys[i])
		r.NoError(err, "Get(%d)", i)
		r.Empty(got, "Get(%d)", i)
	}

	emptyRootStr := expectedRoots[emptyKey]
	expectedHash, err := hex.DecodeString(emptyRootStr)
	r.NoError(err, "Decode expected empty root hash")

	hash, err := proposal.Root()
	r.NoError(err, "%T.Root() after commit", proposal)
	r.Equalf(expectedHash, hash, "%T.Root() of empty trie", db)

	// Commit the proposal.
	err = proposal.Commit()
	r.NoError(err, "Commit")

	// Check that the database is empty.
	hash, err = db.Root()
	r.NoError(err, "%T.Root()", db)
	r.Equalf(expectedHash, hash, "%T.Root() of empty trie", db)
}

func TestDropProposal(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Create a proposal with 10 keys.
	keys, vals := kvForTest(10)
	proposal, err := db.Propose(keys, vals)
	r.NoError(err, "Propose")

	// Drop the proposal.
	err = proposal.Drop()
	r.NoError(err)

	// Check all operations on the dropped proposal.
	err = proposal.Commit()
	r.ErrorIs(err, errDroppedProposal)
	_, err = proposal.Get([]byte("non-existent"))
	r.ErrorIs(err, errDroppedProposal)
	_, err = proposal.Root()
	r.ErrorIs(err, errDroppedProposal)

	// Attempt to "emulate" the proposal to ensure it isn't internally available still.
	proposal = &Proposal{
		handle: db.handle,
		id:     1,
	}

	// Check all operations on the fake proposal.
	_, err = proposal.Get([]byte("non-existent"))
	r.Contains(err.Error(), "proposal not found", "Get(fake proposal)")
	_, err = proposal.Propose([][]byte{[]byte("key")}, [][]byte{[]byte("value")})
	r.Contains(err.Error(), "proposal not found", "Propose(fake proposal)")
	err = proposal.Commit()
	r.Contains(err.Error(), "proposal not found", "Commit(fake proposal)")
}

// Create a proposal with 10 key-value pairs.
// Tests that a proposal can be created from another proposal, and both can be
// committed sequentially.
func TestProposeFromProposal(t *testing.T) {
	r := require.New(t)
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
	r.NoError(err)
	// Create the second proposal from the first.
	proposal2, err := proposal1.Propose(keys2, vals2)
	r.NoError(err)

	// Assert that the first proposal doesn't have keys from the second.
	for i := range keys2 {
		got, err := proposal1.Get(keys2[i])
		r.NoError(err, "Get(%d)", i)
		r.Empty(got, "Get(%d)", i)
	}
	// Assert that the second proposal has keys from the first.
	for i := range keys1 {
		got, err := proposal2.Get(keys1[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(vals1[i], got, "Get(%d)", i)
	}

	// Commit the first proposal.
	err = proposal1.Commit()
	r.NoError(err, "Commit")

	// Assert that the second proposal has keys from the first and second.
	for i := range keys1 {
		got, err := db.Get(keys1[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(vals1[i], got, "Get(%d)", i)
	}
	for i := range keys2 {
		got, err := proposal2.Get(keys2[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(vals2[i], got, "Get(%d)", i)
	}

	// Commit the second proposal.
	err = proposal2.Commit()
	r.NoError(err)

	// Assert that the database has keys from both proposals.
	for i := range keys1 {
		got, err := db.Get(keys1[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(vals1[i], got, "Get(%d)", i)
	}
	for i := range keys2 {
		got, err := db.Get(keys2[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(vals2[i], got, "Get(%d)", i)
	}
}

func TestDeepPropose(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Create a chain of two proposals, each with 10 keys.
	const numKeys = 10
	const numProposals = 10
	proposals := make([]*Proposal, numProposals)
	keys, vals := kvForTest(numKeys * numProposals)

	for i := range proposals {
		var (
			p   *Proposal
			err error
		)
		if i == 0 {
			p, err = db.Propose(keys[i:(i+1)*numKeys], vals[i:(i+1)*numKeys])
			r.NoError(err, "Propose(%d)", i)
		} else {
			p, err = proposals[i-1].Propose(keys[i:(i+1)*numKeys], vals[i:(i+1)*numKeys])
			r.NoError(err, "Propose(%d)", i)
		}
		proposals[i] = p
	}

	// Check that each value is present in the final proposal.
	for i := range keys {
		got, err := proposals[numProposals-1].Get(keys[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(vals[i], got, "Get(%d)", i)
	}

	// Commit each proposal sequentially, and ensure that the values are
	// present in the database after each commit.
	for i := range proposals {
		err := proposals[i].Commit()
		r.NoError(err, "Commit(%d)", i)

		for j := i * numKeys; j < (i+1)*numKeys; j++ {
			got, err := db.Get(keys[j])
			r.NoError(err, "Get(%d)", j)
			r.Equal(vals[j], got, "Get(%d)", j)
		}
	}
}

// Tests that dropping a proposal and committing another one still allows
// access to the data of children proposals
func TestDropProposalAndCommit(t *testing.T) {
	r := require.New(t)
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
			r.NoError(err, "Propose(%d)", i)
		} else {
			p, err = proposals[i-1].Propose(keys[i:(i+1)*numKeys], vals[i:(i+1)*numKeys])
			r.NoError(err, "Propose(%d)", i)
		}
		proposals[i] = p
	}

	// drop the second proposal
	err := proposals[1].Drop()
	r.NoError(err)
	// Commit the first proposal
	err = proposals[0].Commit()
	r.NoError(err)

	// Check that the second proposal is dropped
	_, err = proposals[1].Get(keys[0])
	r.ErrorIs(err, errDroppedProposal, "Get(%d)", 0)

	// Check that all keys can be accessed from the final proposal
	for i := range keys {
		got, err := proposals[numProposals-1].Get(keys[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(vals[i], got, "Get(%d)", i)
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
	r := require.New(t)
	db := newTestDatabase(t)

	// Create two chains of proposals, resulting in the same root.
	keys, vals := kvForTest(10)

	// Create the first proposal chain.
	proposal1, err := db.Propose(keys[0:5], vals[0:5])
	r.NoError(err)
	proposal3Top, err := proposal1.Propose(keys[5:10], vals[5:10])
	r.NoError(err)
	// Create the second proposal chain.
	proposal2, err := db.Propose(keys[5:10], vals[5:10])
	r.NoError(err)
	proposal3Bottom, err := proposal2.Propose(keys[0:5], vals[0:5])
	r.NoError(err)
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
	r.NoError(err)
	proposal5, err := proposal3Bottom.Propose(bottomKeys, bottomVals)
	r.NoError(err)

	// Now we will commit the top chain, and check that the bottom chain is still valid.
	err = proposal1.Commit()
	r.NoError(err)
	err = proposal3Top.Commit()
	r.NoError(err)

	// Check that both final proposals are valid.
	for i := range keys {
		got, err := proposal4.Get(keys[i])
		r.NoError(err, "P4 Get(%d)", i)
		r.Equal(vals[i], got, "P4 Get(%d)", i)
		got, err = proposal5.Get(keys[i])
		r.NoError(err, "P5 Get(%d)", i)
		r.Equal(vals[i], got, "P5 Get(%d)", i)
	}

	// Attempt to commit P5. Since this isn't in the canonical chain, it should
	// fail.
	err = proposal5.Commit()
	r.Contains(err.Error(), "commit the parents of this proposal first") // this error is internal to firewood

	// We should be able to commit P4, since it is in the canonical chain.
	err = proposal4.Commit()
	r.NoError(err)
}

// Tests that an empty revision can be retrieved.
func TestRevision(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals := kvForTest(10)

	// Create a proposal with 10 key-value pairs.
	proposal, err := db.Propose(keys, vals)
	r.NoError(err)

	// Commit the proposal.
	r.NoError(proposal.Commit())

	root, err := db.Root()
	r.NoError(err)

	// Create a revision from this root.
	revision, err := db.Revision(root)
	r.NoError(err)

	// Check that all keys can be retrieved from the revision.
	for i := range keys {
		got, err := revision.Get(keys[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(valForTest(i), got, "Get(%d)", i)
	}

	// Create a second proposal with 10 key-value pairs.
	keys2 := make([][]byte, 10)
	vals2 := make([][]byte, 10)
	for i := range keys2 {
		keys2[i] = keyForTest(i + 10)
		vals2[i] = valForTest(i + 10)
	}
	proposal2, err := db.Propose(keys2, vals2)
	r.NoError(err)
	// Commit the proposal.
	err = proposal2.Commit()
	r.NoError(err)

	// Create a "new" revision from the first old root.
	revision, err = db.Revision(root)
	r.NoError(err)
	// Check that all keys can be retrieved from the revision.
	for i := range keys {
		got, err := revision.Get(keys[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(valForTest(i), got, "Get(%d)", i)
	}
}

func TestInvalidRevision(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Create a nil revision.
	_, err := db.Revision(nil)
	r.ErrorIs(err, errInvalidRootLength)

	// Create a fake revision with an invalid root.
	invalidRoot := []byte("not a valid root")
	_, err = db.Revision(invalidRoot)
	r.ErrorIs(err, errInvalidRootLength)

	// Create a fake revision with an valid root.
	validRoot := []byte("counting 32 bytes to make a hash")
	r.Len(validRoot, 32, "valid root")
	_, err = db.Revision(validRoot)
	r.ErrorIs(err, errRevisionNotFound, "Revision(valid root)")
}

// Tests that edge case `Get` calls are handled correctly.
func TestGetNilCases(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Commit 10 key-value pairs.
	keys, vals := kvForTest(20)
	root, err := db.Update(keys[:10], vals[:10])
	r.NoError(err)

	// Create the other views
	proposal, err := db.Propose(keys[10:], vals[10:])
	r.NoError(err)
	revision, err := db.Revision(root)
	r.NoError(err)

	// Create edge case keys.
	specialKeys := [][]byte{
		nil,
		{}, // empty slice
	}
	for _, k := range specialKeys {
		got, err := db.Get(k)
		r.NoError(err, "db.Get(%q)", k)
		r.Empty(got, "db.Get(%q)", k)

		got, err = revision.Get(k)
		r.NoError(err, "Revision.Get(%q)", k)
		r.Empty(got, "Revision.Get(%q)", k)

		got, err = proposal.Get(k)
		r.NoError(err, "Proposal.Get(%q)", k)
		r.Empty(got, "Proposal.Get(%q)", k)
	}
}

func TestEmptyProposals(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	numProposals := 10
	emptyProposals := make([]*Proposal, numProposals)

	// Create several empty proposals
	for i := range numProposals {
		// Create a proposal with no keys.
		var err error
		if i == 0 {
			emptyProposals[i], err = db.Propose(nil, nil)
		} else {
			emptyProposals[i], err = emptyProposals[i-1].Propose(nil, nil)
		}
		r.NoError(err, "Propose(%d)", i)

		// Check that the proposal has no keys.
		got, err := emptyProposals[i].Get([]byte("non-existent"))
		r.NoError(err, "Get(%d)", i)
		r.Empty(got, "Get(%d)", i)
	}

	// Create one non-empty proposal.
	keys, vals := kvForTest(10)
	nonEmptyProposal, err := db.Propose(keys, vals)
	r.NoError(err, "Propose non-empty proposal")

	// Check that the proposal has the keys we just inserted.
	for i := range keys {
		got, err := nonEmptyProposal.Get(keys[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(vals[i], got, "Get(%d)", i)
	}

	// Commit all empty proposals.
	for _, p := range emptyProposals {
		r.NoError(p.Commit(), "Commit empty proposal")
	}

	// Commit the empty proposal.
	err = nonEmptyProposal.Commit()
	r.NoError(err)

	// Check that the database has the keys from the non-empty proposal.
	for i := range keys {
		got, err := db.Get(keys[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(vals[i], got, "Get(%d)", i)
	}
}

// Tests the GetFromRoot function for retrieving values from specific root hashes.
func TestGetFromRoot(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Check empty database
	emptyRoot, err := db.Root()
	r.NoError(err, "Root of empty database")
	got, err := db.GetFromRoot(emptyRoot, []byte("non-existent"))
	r.NoError(err, "GetFromRoot empty root")
	r.Empty(got, "GetFromRoot empty root should return empty value")

	// Insert some initial data
	keys, vals := kvForTest(10)
	committedRoot, err := db.Update(keys[:5], vals[:5])
	r.NoError(err)

	// Test getting values from first state
	for i := range 5 {
		got, err := db.GetFromRoot(committedRoot, keys[i])
		r.NoError(err, "GetFromRoot committed key %d", i)
		r.Equal(vals[i], got, "GetFromRoot committed key %d", i)
	}

	// Replace the first 5 keys with new values
	p, err := db.Propose(keys[:5], vals[5:])
	r.NoError(err, "Propose to update first 5 keys")
	r.NotNil(p)

	proposedRoot, err := p.Root()
	t.Logf("%x", proposedRoot)
	r.NoError(err, "Root of proposal")

	// Test that we can still get old values from the first root
	for i := range 5 {
		got, err := db.GetFromRoot(committedRoot, keys[i])
		r.NoError(err, "GetFromRoot root1 after update, key %d", i)
		r.Equal(vals[i], got, "GetFromRoot root1 after update, key %d", i)

		got, err = db.GetFromRoot(proposedRoot, keys[i])
		r.NoError(err, "GetFromRoot root1 newer key %d", i)
		r.Equal(vals[i+5], got, "GetFromRoot root1 newer key %d", i)
	}

	// Test with invalid root hash
	invalidRoot := []byte("this is not a valid 32-byte hash")
	_, err = db.GetFromRoot(invalidRoot, []byte("key"))
	r.Error(err, "GetFromRoot with invalid root should return error")

	// Test with valid-length but non-existent root
	nonExistentRoot := make([]byte, RootLength)
	for i := range nonExistentRoot {
		nonExistentRoot[i] = 0xFF // All 1's, very unlikely to exist
	}
	_, err = db.GetFromRoot(nonExistentRoot, []byte("key"))
	r.Error(err, "GetFromRoot with non-existent root should return error")
}
