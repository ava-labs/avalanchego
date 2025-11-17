// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	ethhashKey        = "ethhash"
	firewoodKey       = "firewood"
	emptyKey          = "empty"
	insert100Key      = "100"
	emptyEthhashRoot  = "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	emptyFirewoodRoot = "0000000000000000000000000000000000000000000000000000000000000000"
	errWrongParent    = "The proposal cannot be committed since it is not a direct child of the most recent commit. "
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

func stringToHash(t *testing.T, s string) Hash {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	require.Len(t, b, RootLength)
	return Hash(b)
}

func inferHashingMode(ctx context.Context) (string, error) {
	dbFile := filepath.Join(os.TempDir(), "test.db")
	db, err := newDatabase(dbFile)
	if err != nil {
		return "", err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = db.Close(ctx)
		_ = os.Remove(dbFile)
	}()

	actualEmptyRoot, err := db.Root()
	if err != nil {
		return "", fmt.Errorf("failed to get root of empty database: %w", err)
	}
	actualEmptyRootHex := hex.EncodeToString(actualEmptyRoot[:])

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
		inferredHashMode, err := inferHashingMode(context.Background())
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

// oneSecCtx returns `tb.Context()` with a 1-second timeout added. Any existing
// cancellation on `tb.Context()` is removed, which allows this function to be
// used inside a `tb.Cleanup()`
func oneSecCtx(tb testing.TB) context.Context {
	ctx := context.WithoutCancel(tb.Context())
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	tb.Cleanup(cancel)
	return ctx
}

func newTestDatabase(t *testing.T, configureFns ...func(*Config)) *Database {
	t.Helper()
	r := require.New(t)

	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, err := newDatabase(dbFile, configureFns...)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(db.Close(oneSecCtx(t)))
	})
	return db
}

func newDatabase(dbFile string, configureFns ...func(*Config)) (*Database, error) {
	conf := DefaultConfig()
	conf.Truncate = true // in tests, we use filepath.Join, which creates an empty file
	for _, fn := range configureFns {
		fn(conf)
	}

	f, err := New(dbFile, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to create new database at filepath %q: %w", dbFile, err)
	}
	return f, nil
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
	r.NoError(db.Close(oneSecCtx(t)))

	// Reopen the database with truncate enabled.
	db, err = New(dbFile, config)
	r.NoError(err)

	// Check that the database is empty after truncation.
	hash, err := db.Root()
	r.NoError(err)
	expectedHash := stringToHash(t, expectedRoots[emptyKey])
	r.Equal(expectedHash, hash, "Root hash mismatch after truncation")

	r.NoError(db.Close(oneSecCtx(t)))
}

func TestClosedDatabase(t *testing.T) {
	r := require.New(t)
	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, err := newDatabase(dbFile)
	r.NoError(err)

	r.NoError(db.Close(oneSecCtx(t)))

	_, err = db.Root()
	r.ErrorIs(err, errDBClosed)

	root, err := db.Update(
		[][]byte{[]byte("key")},
		[][]byte{[]byte("value")},
	)
	r.Empty(root)
	r.ErrorIs(err, errDBClosed)

	r.NoError(db.Close(oneSecCtx(t)))
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
	_ = sortKV(keys, vals)
	return keys, vals
}

// sortKV sorts keys lexicographically and keeps vals paired.
func sortKV(keys, vals [][]byte) error {
	if len(keys) != len(vals) {
		return errors.New("keys/vals length mismatch")
	}
	n := len(keys)
	if n <= 1 {
		return nil
	}
	ord := make([]int, n)
	for i := range ord {
		ord[i] = i
	}
	slices.SortFunc(ord, func(i, j int) int {
		return bytes.Compare(keys[i], keys[j])
	})
	perm := make([]int, n)
	for dest, orig := range ord {
		perm[orig] = dest
	}
	for i := 0; i < n; i++ {
		for perm[i] != i {
			j := perm[i]
			keys[i], keys[j] = keys[j], keys[i]
			vals[i], vals[j] = vals[j], vals[i]
			perm[i], perm[j] = perm[j], j
		}
	}
	return nil
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
		Root() (Hash, error)
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

			// Assert the hash is exactly as expected. Test failure indicates a
			// non-hash compatible change has been made since the string was set.
			// If that's expected, update the string at the top of the file to
			// fix this test.
			expectedHash := stringToHash(t, expectedRoots[insert100Key])
			r.Equal(expectedHash, hash, "Root hash mismatch.\nExpected (hex): %x\nActual (hex): %x", expectedHash, hash)
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

	expectedHash := stringToHash(t, expectedRoots[emptyKey])
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
		r.Contains(err.Error(), errWrongParent, "Commit(%d)", i)
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

	expectedHash := stringToHash(t, expectedRoots[emptyKey])
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
	r.NoError(err, "Root of dropped proposal should still be accessible")

	// Check that the keys are not in the database.
	for i := range keys {
		got, err := db.Get(keys[i])
		r.NoError(err, "Get(%d)", i)
		r.Empty(got, "Get(%d)", i)
	}
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
	r.Contains(err.Error(), errWrongParent) // this error is internal to firewood

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
	t.Cleanup(func() {
		r.NoError(revision.Drop())
	})

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
	r.Equal(revision.Root(), root)
	t.Cleanup(func() {
		r.NoError(revision.Drop())
	})
	// Check that all keys can be retrieved from the revision.
	for i := range keys {
		got, err := revision.Get(keys[i])
		r.NoError(err, "Get(%d)", i)
		r.Equal(valForTest(i), got, "Get(%d)", i)
	}
}

// Tests that even if a proposal is committed, the corresponding revision will not go away
// as we're holding on to it
func TestRevisionOutlivesProposal(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals := kvForTest(20)
	_, err := db.Update(keys[:10], vals[:10])
	r.NoError(err)

	// Create a proposal with 10 key-value pairs.
	nKeys, nVals := keys[10:], vals[10:]
	proposal, err := db.Propose(nKeys, nVals)
	r.NoError(err)
	root, err := proposal.Root()
	r.NoError(err)

	rev, err := db.Revision(root)
	r.NoError(err)

	// we drop the proposal
	r.NoError(proposal.Drop())

	// revision should outlive the proposal, as we're still referencing its node store
	for i, key := range nKeys {
		val, err := rev.Get(key)
		r.NoError(err)
		r.Equal(val, nVals[i])
	}

	r.NoError(rev.Drop())
}

// Tests that holding a reference to revision will prevent from it being reaped
func TestRevisionOutlivesReaping(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t, func(config *Config) {
		config.Revisions = 2
	})

	keys, vals := kvForTest(40)
	firstRoot, err := db.Update(keys[:10], vals[:10])
	r.NoError(err)
	// let's get a revision at root
	rev, err := db.Revision(firstRoot)
	r.NoError(err)

	// commit two times, this would normally reap the first revision
	secondRoot, err := db.Update(keys[10:20], vals[10:20])
	r.NoError(err)
	_, err = db.Update(keys[20:30], vals[20:30])
	r.NoError(err)

	// revision should be still accessible, as we're hanging on to it, prevent reaping
	nKeys, nVals := keys[:10], vals[:10]
	for i, key := range nKeys {
		val, err := rev.Get(key)
		r.NoError(err)
		r.Equal(val, nVals[i])
	}
	r.NoError(rev.Drop())

	// since we dropped the revision, if we commit, reaping will happen (cleaning first two revisions)
	_, err = db.Update(keys[30:], vals[30:])
	r.NoError(err)

	_, err = db.Revision(firstRoot)
	r.Error(err)

	_, err = db.Revision(secondRoot)
	r.Error(err)
}

func TestInvalidRevision(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Create a fake revision with an valid root.
	validRoot := Hash([]byte("counting 32 bytes to make a hash"))
	_, err := db.Revision(validRoot)
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
	t.Cleanup(func() {
		r.NoError(revision.Drop())
	})

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

	// Test with valid-length but non-existent root
	var nonExistentRoot Hash
	for i := range nonExistentRoot {
		nonExistentRoot[i] = 0xFF // All 1's, very unlikely to exist
	}
	_, err = db.GetFromRoot(nonExistentRoot, []byte("key"))
	r.Error(err, "GetFromRoot with non-existent root should return error")
}

func TestGetFromRootParallel(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	key, val := []byte("key"), []byte("value")
	allKeys, allVals := kvForTest(10000) // large number to encourage contention
	allKeys = append(allKeys, key)
	allVals = append(allVals, val)

	// Create a proposal
	p, err := db.Propose(allKeys, allVals)
	r.NoError(err, "Propose key-value pairs")

	root, err := p.Root()
	r.NoError(err, "Root of proposal")

	// Use multiple goroutines to stress test concurrent access
	const numReaders = 10
	results := make(chan error, numReaders)
	finish := make(chan struct{})
	wg := sync.WaitGroup{}

	readLots := func(readerID int) error {
		for j := 0; ; j++ {
			got, err := db.GetFromRoot(root, key)
			if err != nil {
				return fmt.Errorf("reader %d, iteration %d: GetFromRoot error: %w", readerID, j, err)
			}
			if !bytes.Equal(got, val) {
				return fmt.Errorf("reader %d, iteration %d: expected %q, got %q", readerID, j, val, got)
			}

			// Add small delays and yield to increase race chances
			if j%100 == 0 {
				runtime.Gosched()
			}
			select {
			case <-finish:
				return nil // Exit if finish signal received
			default:
			}
		}
	}

	// Start multiple reader goroutines
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			results <- readLots(readerID)
			wg.Done()
		}(i)
	}

	// Add small delay to let readers start
	time.Sleep(time.Microsecond * 500)
	t.Log("Committing proposal to allow readers to access data")
	require.NoError(t, p.Commit(), "Commit proposal")
	t.Log("Proposal committed, readers should now access data")
	close(finish) // Signal readers to finish

	// Wait for all readers to finish
	wg.Wait()

	// Collect all results
	for i := 0; i < numReaders; i++ {
		err := <-results
		r.NoError(err, "Parallel operation failed")
	}
}

func TestHandlesFreeImplicitly(t *testing.T) {
	t.Parallel()

	db, err := newDatabase(filepath.Join(t.TempDir(), "test_GC_drops_implicitly.db"))
	require.NoError(t, err)

	// make the db non-empty
	_, err = db.Update([][]byte{keyForTest(1)}, [][]byte{valForTest(1)})
	require.NoError(t, err)

	var (
		explicitlyDropped []any
		implicitlyDropped []any
	)

	// Grab one revision initially. All proposals can also be revisions, so
	// this is the only case in which we don't add both a proposal and a revision.
	historicExplicit, err := db.LatestRevision()
	require.NoErrorf(t, err, "%T.LatestRevision()", db)
	require.NoError(t, historicExplicit.Drop())
	explicitlyDropped = append(explicitlyDropped, historicExplicit)
	historicImplicit, err := db.LatestRevision()
	require.NoErrorf(t, err, "%T.LatestRevision()", db)
	implicitlyDropped = append(implicitlyDropped, historicImplicit)

	// These MUST NOT be committed nor dropped as they demonstrate that the GC
	// finalizer does it for us.
	p0, err := db.Propose(kvForTest(1))
	require.NoErrorf(t, err, "%T.Propose(...)", db)
	root0, err := p0.Root()
	require.NoErrorf(t, err, "%T.Root()", p0)
	rev0, err := db.Revision(root0)
	require.NoErrorf(t, err, "%T.Revision(...)", db)
	implicitlyDropped = append(implicitlyDropped, p0, rev0)
	p1, err := p0.Propose(kvForTest(1))
	require.NoErrorf(t, err, "%T.Propose(...)", p0)
	root1, err := p1.Root()
	require.NoErrorf(t, err, "%T.Root()", p1)
	rev1, err := db.Revision(root1)
	require.NoErrorf(t, err, "%T.Revision(...)", db)
	implicitlyDropped = append(implicitlyDropped, p1, rev1)

	// Demonstrates that explicit [Proposal.Commit] and [Proposal.Drop] calls
	// are sufficient to unblock [Database.Close].
	// Each proposal has a corresponding revision to drop as well.
	for name, free := range map[string](func(*Proposal) error){
		"Commit": (*Proposal).Commit,
		"Drop":   (*Proposal).Drop,
	} {
		p, err := db.Propose(kvForTest(1))
		require.NoErrorf(t, err, "%T.Propose(...)", db)
		root, err := p.Root()
		require.NoErrorf(t, err, "%T.Root()", p)
		rev, err := db.Revision(root)
		require.NoErrorf(t, err, "%T.Revision(...)", db)
		require.NoErrorf(t, free(p), "%T.%s()", p, name)
		require.NoErrorf(t, rev.Drop(), "%T.Drop()", rev)
		explicitlyDropped = append(explicitlyDropped, p, rev)
	}

	done := make(chan struct{})
	go func() {
		require.NoErrorf(t, db.Close(oneSecCtx(t)), "%T.Close()", db)
		close(done)
	}()

	select {
	case <-done:
		require.Failf(t, "Unexpected return", "%T.Close() returned with undropped %T", db, p0)
	case <-time.After(300 * time.Millisecond):
		// TODO(arr4n) use `synctest` package when at Go 1.25
	}

	// This is the last time we must guarantee that these handles are reachable.
	// After this, they must be unreachable so that the GC can collect them.
	for _, h := range implicitlyDropped {
		runtime.KeepAlive(h)
	}

	// In practice there's no need to call [runtime.GC] if [Database.Close] is
	// called after all proposals are unreachable, as it does it itself.
	runtime.GC()
	// Note that [Database.Close] waits for outstanding handles, so this would
	// block permanently if the unreachability of implicitly dropped handles didn't
	// result in their Drop methods being called.
	<-done

	for _, p := range explicitlyDropped {
		runtime.KeepAlive(p)
	}
}

type kvIter interface {
	SetBatchSize(int)
	Next() bool
	Key() []byte
	Value() []byte
	Err() error
	Drop() error
}
type borrowIter struct{ it *Iterator }

func (b *borrowIter) SetBatchSize(batchSize int) { b.it.SetBatchSize(batchSize) }
func (b *borrowIter) Next() bool                 { return b.it.NextBorrowed() }
func (b *borrowIter) Key() []byte                { return b.it.Key() }
func (b *borrowIter) Value() []byte              { return b.it.Value() }
func (b *borrowIter) Err() error                 { return b.it.Err() }
func (b *borrowIter) Drop() error                { return b.it.Drop() }

func assertIteratorYields(r *require.Assertions, it kvIter, keys [][]byte, vals [][]byte) {
	i := 0
	for ; it.Next(); i += 1 {
		r.Equal(keys[i], it.Key())
		r.Equal(vals[i], it.Value())
	}
	r.NoError(it.Err())
	r.Equal(len(keys), i)
}

type iteratorConfigFn = func(it kvIter) kvIter

var iterConfigs = map[string]iteratorConfigFn{
	"Owned":    func(it kvIter) kvIter { return it },
	"Borrowed": func(it kvIter) kvIter { return &borrowIter{it: it.(*Iterator)} },
	"Single": func(it kvIter) kvIter {
		it.SetBatchSize(1)
		return it
	},
	"Batched": func(it kvIter) kvIter {
		it.SetBatchSize(100)
		return it
	},
}

func runIteratorTestForModes(t *testing.T, fn func(*testing.T, iteratorConfigFn), modes ...string) {
	testName := strings.Join(modes, "/")
	t.Run(testName, func(t *testing.T) {
		r := require.New(t)
		fn(t, func(it kvIter) kvIter {
			for _, m := range modes {
				config, ok := iterConfigs[m]
				r.Truef(ok, "specified config mode %s does not exist", m)
				it = config(it)
			}
			return it
		})
	})
}

func runIteratorTestForAllModes(parentT *testing.T, fn func(*testing.T, iteratorConfigFn)) {
	for _, dataMode := range []string{"Owned", "Borrowed"} {
		for _, batchMode := range []string{"Single", "Batched"} {
			runIteratorTestForModes(parentT, fn, batchMode, dataMode)
		}
	}
}

// Tests that basic iterator functionality works
func TestIter(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals := kvForTest(100)
	_, err := db.Update(keys, vals)
	r.NoError(err)

	runIteratorTestForAllModes(t, func(t *testing.T, cfn iteratorConfigFn) {
		r := require.New(t)
		rev, err := db.LatestRevision()
		r.NoError(err)
		it, err := rev.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(it.Drop())
			r.NoError(rev.Drop())
		})

		assertIteratorYields(r, cfn(it), keys, vals)
	})
}

func TestIterOnRoot(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals := kvForTest(240)
	firstRoot, err := db.Update(keys[:80], vals[:80])
	r.NoError(err)
	secondRoot, err := db.Update(keys[80:160], vals[80:160])
	r.NoError(err)
	thirdRoot, err := db.Update(keys[160:], vals[160:])
	r.NoError(err)

	runIteratorTestForAllModes(t, func(t *testing.T, cfn iteratorConfigFn) {
		r := require.New(t)
		r1, err := db.Revision(firstRoot)
		r.NoError(err)
		h1, err := r1.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(h1.Drop())
			r.NoError(r1.Drop())
		})

		r2, err := db.Revision(secondRoot)
		r.NoError(err)
		h2, err := r2.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(h2.Drop())
			r.NoError(r2.Drop())
		})

		r3, err := db.Revision(thirdRoot)
		r.NoError(err)
		h3, err := r3.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(h3.Drop())
			r.NoError(r3.Drop())
		})

		assertIteratorYields(r, cfn(h1), keys[:80], vals[:80])
		assertIteratorYields(r, cfn(h2), keys[:160], vals[:160])
		assertIteratorYields(r, cfn(h3), keys, vals)
	})
}

func TestIterOnProposal(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals := kvForTest(240)
	_, err := db.Update(keys, vals)
	r.NoError(err)

	runIteratorTestForAllModes(t, func(t *testing.T, cfn iteratorConfigFn) {
		r := require.New(t)
		updatedValues := make([][]byte, len(vals))
		copy(updatedValues, vals)

		changedKeys := make([][]byte, 0)
		changedVals := make([][]byte, 0)
		for i := 0; i < len(vals); i += 4 {
			changedKeys = append(changedKeys, keys[i])
			newVal := []byte{byte(i)}
			changedVals = append(changedVals, newVal)
			updatedValues[i] = newVal
		}
		p, err := db.Propose(changedKeys, changedVals)
		r.NoError(err)
		it, err := p.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(it.Drop())
		})

		assertIteratorYields(r, cfn(it), keys, updatedValues)
	})
}

// Tests that the iterator still works after proposal is committed
func TestIterAfterProposalCommit(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals := kvForTest(10)
	p, err := db.Propose(keys, vals)
	r.NoError(err)

	it, err := p.Iter(nil)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(it.Drop())
	})

	err = p.Commit()
	r.NoError(err)

	// iterate after commit
	// because iterator hangs on the nodestore reference of proposal
	// the nodestore won't be dropped until we drop the iterator
	assertIteratorYields(r, it, keys, vals)
}

// Tests that the iterator on latest revision works properly after a proposal commit
func TestIterUpdate(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals := kvForTest(10)
	_, err := db.Update(keys, vals)
	r.NoError(err)

	// get an iterator on latest revision
	rev, err := db.LatestRevision()
	r.NoError(err)
	it, err := rev.Iter(nil)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(it.Drop())
		r.NoError(rev.Drop())
	})

	// update the database
	keys2, vals2 := kvForTest(10)
	_, err = db.Update(keys2, vals2)
	r.NoError(err)

	// iterate after commit
	// because iterator is fixed on the revision hash, it should return the initial values
	assertIteratorYields(r, it, keys, vals)
}

// Tests the iterator's behavior after exhaustion, should safely return empty item/batch, indicating done
func TestIterDone(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals := kvForTest(18)
	_, err := db.Update(keys, vals)
	r.NoError(err)

	// get an iterator on latest revision
	rev, err := db.LatestRevision()
	r.NoError(err)
	it, err := rev.Iter(nil)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(it.Drop())
		r.NoError(rev.Drop())
	})
	// consume the iterator
	assertIteratorYields(r, it, keys, vals)
	// calling next again should be safe and return false
	r.False(it.Next())
	r.NoError(it.Err())

	// get a new iterator
	it2, err := rev.Iter(nil)
	r.NoError(err)
	// set batch size to 5
	it2.SetBatchSize(5)
	// consume the iterator
	assertIteratorYields(r, it2, keys, vals)
	// calling next again should be safe and return false
	r.False(it.Next())
	r.NoError(it.Err())
}

// TestNilVsEmptyValue tests that nil values cause delete operations while
// empty []byte{} values result in inserts with empty values.
func TestNilVsEmptyValue(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Insert a key with a non-empty value
	key1 := []byte("key1")
	value1 := []byte("value1")
	_, err := db.Update([][]byte{key1}, [][]byte{value1})
	r.NoError(err, "Insert key1 with value1")

	// Verify the key exists
	got, err := db.Get(key1)
	r.NoError(err, "Get key1")
	r.Equal(value1, got, "key1 should have value1")

	// Insert another key with an empty value (not nil)
	key2 := []byte("key2")
	emptyValue := []byte{} // empty slice, not nil
	_, err = db.Update([][]byte{key2}, [][]byte{emptyValue})
	r.NoError(err, "Insert key2 with empty value")

	// Verify key2 exists with empty value
	got, err = db.Get(key2)
	r.NoError(err, "Get key2")
	r.NotNil(got, "key2 should exist (not be nil)")
	r.Empty(got, "key2 should have empty value")

	// Now use nil value to delete key1 (DeleteRange operation)
	var nilValue []byte = nil
	_, err = db.Update([][]byte{key1}, [][]byte{nilValue})
	r.NoError(err, "Delete key1 with nil value")

	// Verify key1 is deleted
	got, err = db.Get(key1)
	r.NoError(err, "Get key1 after delete")
	r.Nil(got, "key1 should be deleted")

	// Verify key2 still exists with empty value
	got, err = db.Get(key2)
	r.NoError(err, "Get key2 after key1 delete")
	r.NotNil(got, "key2 should still exist")
	r.Empty(got, "key2 should still have empty value")

	// Test with batch operations
	key3 := []byte("key3")
	value3 := []byte("value3")
	key4 := []byte("key4")
	emptyValue4 := []byte{}

	_, err = db.Update(
		[][]byte{key3, key4},
		[][]byte{value3, emptyValue4},
	)
	r.NoError(err, "Batch insert key3 and key4")

	// Verify both keys exist
	got, err = db.Get(key3)
	r.NoError(err, "Get key3")
	r.Equal(value3, got, "key3 should have value3")

	got, err = db.Get(key4)
	r.NoError(err, "Get key4")
	r.NotNil(got, "key4 should exist")
	r.Empty(got, "key4 should have empty value")
}

// TestCloseWithCancelledContext verifies that Database.Close returns
// ErrActiveKeepAliveHandles when the context is cancelled before handles are dropped.
func TestCloseWithCancelledContext(t *testing.T) {
	r := require.New(t)
	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, err := newDatabase(dbFile)
	r.NoError(err)

	// Create a proposal to keep a handle active
	keys, vals := kvForTest(1)
	proposal, err := db.Propose(keys, vals)
	r.NoError(err)

	ctx, cancel := context.WithCancel(t.Context())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		err = db.Close(ctx)
	}()

	cancel()
	wg.Wait()

	r.ErrorIs(err, ErrActiveKeepAliveHandles, "Close should return ErrActiveKeepAliveHandles when context is cancelled")
	r.ErrorIs(err, context.Canceled, "Close error should wrap context.Canceled")

	// Drop the proposal
	r.NoError(proposal.Drop())

	// Now Close should succeed
	r.NoError(db.Close(oneSecCtx(t)))
}

// TestCloseWithMultipleActiveHandles verifies that Database.Close returns
// ErrActiveKeepAliveHandles when multiple handles are active and context is cancelled.
func TestCloseWithMultipleActiveHandles(t *testing.T) {
	r := require.New(t)
	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, err := newDatabase(dbFile)
	r.NoError(err)

	// Create multiple proposals
	keys1, vals1 := kvForTest(3)
	proposal1, err := db.Propose(keys1[:1], vals1[:1])
	r.NoError(err)
	proposal2, err := db.Propose(keys1[1:2], vals1[1:2])
	r.NoError(err)
	proposal3, err := db.Propose(keys1[2:3], vals1[2:3])
	r.NoError(err)

	// Create multiple revisions
	root1, err := db.Update([][]byte{keyForTest(10)}, [][]byte{valForTest(10)})
	r.NoError(err)
	root2, err := db.Update([][]byte{keyForTest(20)}, [][]byte{valForTest(20)})
	r.NoError(err)

	revision1, err := db.Revision(root1)
	r.NoError(err)
	revision2, err := db.Revision(root2)
	r.NoError(err)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Close should return ErrActiveKeepAliveHandles
	err = db.Close(ctx)
	r.ErrorIs(err, ErrActiveKeepAliveHandles, "Close should return ErrActiveKeepAliveHandles with multiple active handles")
	r.ErrorIs(err, context.Canceled, "Close error should wrap context.Canceled")

	// Drop all handles
	r.NoError(proposal1.Drop())
	r.NoError(proposal2.Drop())
	r.NoError(proposal3.Drop())
	r.NoError(revision1.Drop())
	r.NoError(revision2.Drop())

	// Now Close should succeed
	r.NoError(db.Close(oneSecCtx(t)))
}

// TestCloseSucceedsWhenHandlesDroppedInTime verifies that Database.Close succeeds
// when all handles are dropped before the context timeout.
func TestCloseSucceedsWhenHandlesDroppedInTime(t *testing.T) {
	r := require.New(t)
	dbFile := filepath.Join(t.TempDir(), "test.db")
	db, err := newDatabase(dbFile)
	r.NoError(err)

	// Create two active proposals
	keys, vals := kvForTest(2)
	proposal1, err := db.Propose(keys[:1], vals[:1])
	r.NoError(err)
	proposal2, err := db.Propose(keys[1:2], vals[1:2])
	r.NoError(err)

	// Channel to receive Close result
	closeDone := make(chan error, 1)

	// Start Close in a goroutine
	go func() {
		closeDone <- db.Close(oneSecCtx(t))
	}()

	// Drop handles after a short delay (before timeout)
	time.Sleep(100 * time.Millisecond)
	r.NoError(proposal1.Drop())
	r.NoError(proposal2.Drop())
	r.NoError(<-closeDone, "Close should succeed when handles are dropped before timeout")
}
