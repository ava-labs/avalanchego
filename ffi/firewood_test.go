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
	dbDir := os.TempDir()
	db, err := newDatabase(dbDir)
	if err != nil {
		return "", err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = db.Close(ctx)
		_ = os.Remove(dbDir)
	}()

	actualEmptyRoot := db.Root()
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

func newTestDatabase(tb testing.TB, opts ...Option) *Database {
	tb.Helper()
	r := require.New(tb)

	db, err := newDatabase(tb.TempDir(), opts...)
	r.NoError(err)
	tb.Cleanup(func() {
		err := db.Close(oneSecCtx(tb))
		if errors.Is(err, ErrActiveKeepAliveHandles) {
			// force a GC to clean up dangling handles that are preventing the
			// database from closing, then try again. Intentionally not looping
			// since a subsequent attempt is unlikely to succeed if the first
			// one didn't.
			runtime.GC()
			err = db.Close(oneSecCtx(tb))
		}
		r.NoError(err)
	})
	return db
}

var (
	// detectedNodeHashAlgorithm is cached after first detection to avoid repeated attempts
	detectedNodeHashAlgorithm     NodeHashAlgorithm
	detectedNodeHashAlgorithmOnce sync.Once
)

func newDatabase(dbDir string, opts ...Option) (*Database, error) {
	// Detect the correct algo once and cache it
	detectedNodeHashAlgorithmOnce.Do(func() {
		// Try ethhash first, if it fails try merkledb
		tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("firewood-hash-detection-%d", time.Now().UnixNano()))
		defer os.RemoveAll(tempDir)

		_, err := New(tempDir, EthereumNodeHashing, WithTruncate(true))
		if err == nil {
			detectedNodeHashAlgorithm = EthereumNodeHashing
		} else {
			detectedNodeHashAlgorithm = MerkleDBNodeHashing
		}
	})

	f, err := New(dbDir, detectedNodeHashAlgorithm, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create new database in directory %q: %w", dbDir, err)
	}
	return f, nil
}

func TestUpdateSingleKV(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals, batch := kvForTest(1)
	_, err := db.Update(batch)
	r.NoError(err)

	got, err := db.Get(keys[0])
	r.NoError(err)
	r.Equal(vals[0], got)
}

func TestNodeHashAlgorithmValidation(t *testing.T) {
	r := require.New(t)

	// Try to create a database with ethhash
	dbDirEth := t.TempDir()
	dbEth, errEth := New(dbDirEth, EthereumNodeHashing, WithTruncate(true))

	// Try to create a database with merkledb
	dbDirMerkledb := t.TempDir()
	dbMerkledb, errMerkledb := New(dbDirMerkledb, MerkleDBNodeHashing, WithTruncate(true))

	// Exactly one should succeed based on compile-time feature
	type resultType int
	const (
		ethereumOnly resultType = iota
		merkledbOnly
		bothOrNeither
	)

	result := bothOrNeither
	switch {
	case errEth == nil && errMerkledb != nil:
		result = ethereumOnly
	case errMerkledb == nil && errEth != nil:
		result = merkledbOnly
	}

	switch result {
	case ethereumOnly:
		r.NoError(dbEth.Close(oneSecCtx(t)))
		r.Error(errMerkledb)
		r.ErrorContains(errMerkledb, "node store hash algorithm mismatch: want to initialize with MerkleDB, but build option is for Ethereum")
	case merkledbOnly:
		r.NoError(dbMerkledb.Close(oneSecCtx(t)))
		r.Error(errEth)
		r.ErrorContains(errEth, "node store hash algorithm mismatch: want to initialize with Ethereum, but build option is for MerkleDB")
	case bothOrNeither:
		// Both succeeded or both failed - this should not happen
		r.Failf("Expected exactly one hash type to succeed", "got errEth=%v, errNative=%v", errEth, errMerkledb)
	}
}

func TestUpdateMultiKV(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals, batch := kvForTest(10)
	_, err := db.Update(batch)
	r.NoError(err)

	for i, key := range keys {
		got, err := db.Get(key)
		r.NoError(err)
		r.Equal(vals[i], got)
	}
}

func TestTruncateDatabase(t *testing.T) {
	r := require.New(t)
	dbDir := t.TempDir()
	// Create a new database with truncate enabled.
	db, err := newDatabase(dbDir, WithTruncate(true))
	r.NoError(err)

	// Insert some data.
	_, _, batch := kvForTest(10)
	_, err = db.Update(batch)
	r.NoError(err)

	// Close the database.
	r.NoError(db.Close(oneSecCtx(t)))

	// Reopen the database with truncate enabled.
	db, err = newDatabase(dbDir, WithTruncate(true))
	r.NoError(err)

	// Check that the database is empty after truncation.
	hash := db.Root()
	expectedHash := stringToHash(t, expectedRoots[emptyKey])
	r.Equal(expectedHash, hash, "Root hash mismatch after truncation")

	r.NoError(db.Close(oneSecCtx(t)))
}

func TestClosedDatabase(t *testing.T) {
	r := require.New(t)
	db, err := newDatabase(t.TempDir())
	r.NoError(err)

	r.NoError(db.Close(oneSecCtx(t)))

	root := db.Root()
	r.Equal(EmptyRoot, root)

	root, err = db.Update([]BatchOp{Put([]byte("key"), []byte("value"))})
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

func kvForTest(num int) ([][]byte, [][]byte, []BatchOp) {
	keys := make([][]byte, num)
	vals := make([][]byte, num)
	for i := range keys {
		keys[i] = keyForTest(i)
		vals[i] = valForTest(i)
	}
	_ = sortKV(keys, vals)
	return keys, vals, makeBatch(keys, vals)
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
	for i := range n {
		for perm[i] != i {
			j := perm[i]
			keys[i], keys[j] = keys[j], keys[i]
			vals[i], vals[j] = vals[j], vals[i]
			perm[i], perm[j] = perm[j], j
		}
	}
	return nil
}

func makeBatch(keys, vals [][]byte) []BatchOp {
	batch := make([]BatchOp, len(keys))
	for i := range keys {
		batch[i] = Put(keys[i], vals[i])
	}
	return batch
}

// Tests that 100 key-value pairs can be inserted and retrieved.
// This happens in 3 ways:
// 1. By calling [Database.Propose] and then [Proposal.Commit].
// 2. By calling [Database.Update] directly - no proposal storage is needed.
// 3. By calling [Database.Propose] and not committing, which returns a proposal.
func TestInsert100(t *testing.T) {
	type dbView interface {
		Get(key []byte) ([]byte, error)
		Propose(batch []BatchOp) (*Proposal, error)
		Root() Hash
	}

	tests := []struct {
		name   string
		insert func(*testing.T, dbView, []BatchOp) (dbView, error)
	}{
		{
			name: "Propose and Commit",
			insert: func(_ *testing.T, db dbView, batch []BatchOp) (dbView, error) {
				proposal, err := db.Propose(batch)
				if err != nil {
					return nil, err
				}
				if err := proposal.Commit(); err != nil {
					return nil, err
				}
				return db, nil
			},
		},
		{
			name: "Update",
			insert: func(_ *testing.T, db dbView, batch []BatchOp) (dbView, error) {
				actualDB, ok := db.(*Database)
				if !ok {
					return nil, fmt.Errorf("expected *Database, got %T", db)
				}
				_, err := actualDB.Update(batch)
				return db, err
			},
		},
		{
			name: "Propose",
			insert: func(t *testing.T, db dbView, batch []BatchOp) (dbView, error) {
				proposal, err := db.Propose(batch)
				if err != nil {
					return nil, err
				}
				t.Cleanup(func() {
					require.NoError(t, proposal.Drop())
				})
				return proposal, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			db := newTestDatabase(t)

			keys, vals, batch := kvForTest(100)
			newDB, err := tt.insert(t, db, batch)
			r.NoError(err)

			for i := range keys {
				got, err := newDB.Get(keys[i])
				r.NoError(err)
				// Cast as strings to improve debug messages.
				want := string(vals[i])
				r.Equal(want, string(got))
			}

			// Assert the hash is exactly as expected. Test failure indicates a
			// non-hash compatible change has been made since the string was set.
			// If that's expected, update the string at the top of the file to
			// fix this test.
			expectedHash := stringToHash(t, expectedRoots[insert100Key])
			hash := newDB.Root()
			r.Equal(expectedHash, hash, "Root hash mismatch.\nExpected (hex): %x\nActual (hex): %x", expectedHash, hash)
		})
	}
}

// Tests that a range of keys can be deleted.
func TestRangeDelete(t *testing.T) {
	tests := []struct {
		name   string
		delete func(db *Database, keys [][]byte, prefix []byte) error
	}{
		{
			name: "PrefixDelete",
			delete: func(db *Database, _ [][]byte, prefix []byte) error {
				_, err := db.Update([]BatchOp{PrefixDelete(prefix)})
				return err
			},
		},
		{
			name: "Delete",
			delete: func(db *Database, keys [][]byte, prefix []byte) error {
				// Delete each key that has the prefix individually
				batch := make([]BatchOp, 0)
				for _, key := range keys {
					if bytes.HasPrefix(key, prefix) {
						batch = append(batch, Delete(key))
					}
				}
				_, err := db.Update(batch)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			db := newTestDatabase(t)
			keys, vals, batch := kvForTest(100)
			_, err := db.Update(batch)
			r.NoError(err)

			const deletePrefix = 1
			prefix := keyForTest(deletePrefix)
			r.NoError(tt.delete(db, keys, prefix))

			for i := range keys {
				got, err := db.Get(keys[i])
				r.NoError(err)

				if deleted := bytes.HasPrefix(keys[i], prefix); deleted {
					r.Nil(got)
				} else {
					r.Equal(vals[i], got)
				}
			}
		})
	}
}

// Tests that the database is empty after creation and doesn't panic.
func TestInvariants(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	hash := db.Root()

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
		for j := range numKeys {
			keys[j] = keyForTest(i*numKeys + j)
			vals[j] = valForTest(i*numKeys + j)
		}
		proposal, err := db.Propose(makeBatch(keys, vals))
		r.NoError(err)
		proposals[i] = proposal
	}

	// Check that each value is present in each proposal.
	for i, p := range proposals {
		for j := range numKeys {
			got, err := p.Get(keyForTest(i*numKeys + j))
			r.NoError(err)
			r.Equal(valForTest(i*numKeys+j), got, "Get(%d)", i*numKeys+j)
		}
	}

	// Commit only the first proposal.
	err := proposals[0].Commit()
	r.NoError(err)
	// Check that the first proposal's keys are present.
	for j := range numKeys {
		got, err := db.Get(keyForTest(j))
		r.NoError(err)
		r.Equal(valForTest(j), got, "Get(%d)", j)
	}
	// Check that the other proposals' keys are not present.
	for i := 1; i < numProposals; i++ {
		for j := range numKeys {
			got, err := db.Get(keyForTest(i*numKeys + j))
			r.Empty(got, "Get(%d)", i*numKeys+j)
			r.NoError(err, "Get(%d)", i*numKeys+j)
		}
	}

	// Ensure we can still get values from the other proposals.
	for i := 1; i < numProposals; i++ {
		for j := range numKeys {
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
		for j := range numKeys {
			got, err := proposals[i].Get(keyForTest(i*numKeys + j))
			r.ErrorIs(err, errDroppedProposal, "Get(%d)", i*numKeys+j)
			r.Empty(got, "Get(%d)", i*numKeys+j)
		}
	}
}

// Tests that a proposal that deletes all keys can be committed.
func TestDeleteAll(t *testing.T) {
	tests := []struct {
		name   string
		delete func(db *Database, keys [][]byte, prefix []byte) (*Proposal, error)
	}{
		{
			name: "PrefixDelete",
			delete: func(db *Database, _ [][]byte, prefix []byte) (*Proposal, error) {
				return db.Propose([]BatchOp{PrefixDelete(prefix)})
			},
		},
		{
			name: "Delete",
			delete: func(db *Database, keys [][]byte, prefix []byte) (*Proposal, error) {
				batch := make([]BatchOp, 0, len(keys))
				for _, key := range keys {
					if bytes.HasPrefix(key, prefix) {
						batch = append(batch, Delete(key))
					}
				}
				return db.Propose(batch)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			db := newTestDatabase(t)

			keys, _, batch := kvForTest(10)
			// Insert 10 key-value pairs.
			_, err := db.Update(batch)
			r.NoError(err)

			// Create a proposal that deletes all keys.
			proposal, err := tt.delete(db, keys, []byte("key"))
			r.NoError(err)

			// Check that the proposal doesn't have the keys we just inserted.
			for i := range keys {
				got, err := proposal.Get(keys[i])
				r.NoError(err, "Get(%d)", i)
				r.Empty(got, "Get(%d)", i)
			}

			expectedHash := stringToHash(t, expectedRoots[emptyKey])
			hash := proposal.Root()
			r.Equalf(expectedHash, hash, "%T.Root() of empty trie", db)

			// Commit the proposal.
			err = proposal.Commit()
			r.NoError(err, "Commit")

			// Check that the database is empty.
			hash = db.Root()
			r.Equalf(expectedHash, hash, "%T.Root() of empty trie", db)
		})
	}
}

func TestDropProposal(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Create a proposal with 10 keys.
	keys, _, batch := kvForTest(10)
	proposal, err := db.Propose(batch)
	r.NoError(err, "Propose")

	// Drop the proposal.
	err = proposal.Drop()
	r.NoError(err)

	// Check all operations on the dropped proposal.
	err = proposal.Commit()
	r.ErrorIs(err, errDroppedProposal)
	_, err = proposal.Get([]byte("non-existent"))
	r.ErrorIs(err, errDroppedProposal)
	_ = proposal.Root()

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
	proposal1, err := db.Propose(makeBatch(keys1, vals1))
	r.NoError(err)
	// Create the second proposal from the first.
	proposal2, err := proposal1.Propose(makeBatch(keys2, vals2))
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

	// Create a chain of proposals, each with 10 keys.
	const numKeys = 10
	const numProposals = 10
	proposals := make([]*Proposal, numProposals)
	keys, vals, batch := kvForTest(numKeys * numProposals)

	for i := range proposals {
		var (
			p   *Proposal
			err error
		)
		ops := batch[i*numKeys : (i+1)*numKeys]
		if i == 0 {
			p, err = db.Propose(ops)
			r.NoError(err, "Propose(%d)", i)
		} else {
			p, err = proposals[i-1].Propose(ops)
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
			p, err = db.Propose(makeBatch(keys[i:(i+1)*numKeys], vals[i:(i+1)*numKeys]))
			r.NoError(err, "Propose(%d)", i)
		} else {
			p, err = proposals[i-1].Propose(makeBatch(keys[i:(i+1)*numKeys], vals[i:(i+1)*numKeys]))
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
	keys, vals, batch := kvForTest(10)

	// Create the first proposal chain.
	proposal1, err := db.Propose(batch[0:5])
	r.NoError(err)
	proposal3Top, err := proposal1.Propose(batch[5:10])
	r.NoError(err)
	// Create the second proposal chain.
	proposal2, err := db.Propose(batch[5:10])
	r.NoError(err)
	proposal3Bottom, err := proposal2.Propose(batch[0:5])
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
	proposal4, err := proposal3Top.Propose(makeBatch(topKeys, topVals))
	r.NoError(err)
	proposal5, err := proposal3Bottom.Propose(makeBatch(bottomKeys, bottomVals))
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

	keys, _, batch := kvForTest(10)

	// Create a proposal with 10 key-value pairs.
	proposal, err := db.Propose(batch)
	r.NoError(err)

	// Commit the proposal.
	r.NoError(proposal.Commit())

	root := db.Root()

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
	proposal2, err := db.Propose(makeBatch(keys2, vals2))
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

	keys, vals, batch := kvForTest(20)
	_, err := db.Update(batch[:10])
	r.NoError(err)

	// Create a proposal with 10 key-value pairs.
	nKeys, nVals := keys[10:], vals[10:]
	proposal, err := db.Propose(batch[10:])
	r.NoError(err)
	root := proposal.Root()

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

// Tests that after committing a proposal, the corresponding revision is still accessible.
func TestCommitWithRevisionHeld(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals, batch := kvForTest(20)
	root, err := db.Update(batch[:10])
	r.NoError(err)
	base, err := db.Revision(root)
	r.NoError(err)

	// Create a proposal with 10 key-value pairs.
	proposal, err := db.Propose(batch[10:])
	r.NoError(err)
	root = proposal.Root()

	rev, err := db.Revision(root)
	r.NoError(err)

	// both revisions should be accessible after commit
	r.NoError(proposal.Commit())
	for i, key := range keys {
		val, err := base.Get(key)
		r.NoErrorf(err, "base.Get(): %d", i)
		if i < 10 {
			r.Equalf(vals[i], val, "base.Get(): %d", i)
		} else {
			r.Emptyf(val, "base.Get(): %d", i)
		}
		val, err = rev.Get(key)
		r.NoErrorf(err, "rev.Get(): %d", i)
		r.Equalf(vals[i], val, "rev.Get(): %d", i)
	}
	r.NoError(base.Drop())
	r.NoError(rev.Drop())
}

// Tests that holding a reference to revision will prevent from it being reaped
func TestRevisionOutlivesReaping(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t, WithRevisions(2))

	keys, vals, batch := kvForTest(40)
	firstRoot, err := db.Update(batch[:10])
	r.NoError(err)
	// let's get a revision at root
	rev, err := db.Revision(firstRoot)
	r.NoError(err)

	// commit two times, this would normally reap the first revision
	secondRoot, err := db.Update(batch[10:20])
	r.NoError(err)
	_, err = db.Update(batch[20:30])
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
	_, err = db.Update(batch[30:])
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
	_, _, batch := kvForTest(20)
	root, err := db.Update(batch[:10])
	r.NoError(err)

	// Create the other views
	proposal, err := db.Propose(batch[10:])
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
			emptyProposals[i], err = db.Propose(nil)
		} else {
			emptyProposals[i], err = emptyProposals[i-1].Propose(nil)
		}
		r.NoError(err, "Propose(%d)", i)

		// Check that the proposal has no keys.
		got, err := emptyProposals[i].Get([]byte("non-existent"))
		r.NoError(err, "Get(%d)", i)
		r.Empty(got, "Get(%d)", i)
	}

	// Create one non-empty proposal.
	keys, vals, batch := kvForTest(10)
	nonEmptyProposal, err := db.Propose(batch)
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

func TestHandlesFreeImplicitly(t *testing.T) {
	t.Parallel()

	db, err := newDatabase(t.TempDir())
	require.NoError(t, err)

	// make the db non-empty
	_, err = db.Update([]BatchOp{Put(keyForTest(1), valForTest(1))})
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
	_, _, batch0 := kvForTest(1)
	p0, err := db.Propose(batch0)
	require.NoErrorf(t, err, "%T.Propose(...)", db)
	root0 := p0.Root()
	rev0, err := db.Revision(root0)
	require.NoErrorf(t, err, "%T.Revision(...)", db)
	implicitlyDropped = append(implicitlyDropped, p0, rev0)
	_, _, batch1 := kvForTest(1)
	p1, err := p0.Propose(batch1)
	require.NoErrorf(t, err, "%T.Propose(...)", p0)
	root1 := p1.Root()
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
		_, _, batch := kvForTest(1)
		p, err := db.Propose(batch)
		require.NoErrorf(t, err, "%T.Propose(...)", db)
		root := p.Root()
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

func TestFjallStore(t *testing.T) {
	r := require.New(t)

	// Create a new database with RootStore enabled
	// Setting the number of in-memory revisions to 5 tests that revision nodes
	// are not reaped prior to closing the database.
	dbDir := t.TempDir()
	db, err := newDatabase(dbDir,
		WithRootStore(),
		WithRevisions(5),
	)
	r.NoError(err)

	// Create and commit 10 proposals
	numRevisions := 10
	key := []byte("root_store")
	_, vals, _ := kvForTest(numRevisions)
	revisionRoots := make([]Hash, numRevisions)
	for i := range numRevisions {
		proposal, err := db.Propose([]BatchOp{Put(key, vals[i])})
		r.NoError(err)
		r.NoError(proposal.Commit())

		revisionRoots[i] = proposal.Root()
		r.NoError(err)
	}

	// Close and reopen the database
	r.NoError(db.Close(t.Context()))

	db, err = newDatabase(dbDir,
		WithRootStore(),
		WithRevisions(5),
	)
	r.NoError(err)

	// Verify that we can access all revisions
	for i := range numRevisions {
		revision, err := db.Revision(revisionRoots[i])
		r.NoError(err)

		defer func() {
			r.NoError(revision.Drop())
		}()

		v, err := revision.Get(key)
		r.NoError(err)

		r.Equal(vals[i], v)
		r.NoError(revision.Drop())
	}
}

// TestNilVsEmptyValue tests that empty []byte{} values result in inserts with
// empty values (not missing keys), and that deletes are explicit via BatchOp.
func TestNilVsEmptyValue(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Insert a key with a non-empty value
	key1 := []byte("key1")
	value1 := []byte("value1")
	_, err := db.Update([]BatchOp{Put(key1, value1)})
	r.NoError(err, "Insert key1 with value1")

	// Verify the key exists
	got, err := db.Get(key1)
	r.NoError(err, "Get key1")
	r.Equal(value1, got, "key1 should have value1")

	// Insert another key with an empty value (not nil)
	key2 := []byte("key2")
	emptyValue := []byte{} // empty slice, not nil
	_, err = db.Update([]BatchOp{Put(key2, emptyValue)})
	r.NoError(err, "Insert key2 with empty value")

	// Verify key2 exists with empty value
	got, err = db.Get(key2)
	r.NoError(err, "Get key2")
	r.NotNil(got, "key2 should exist (not be nil)")
	r.Empty(got, "key2 should have empty value")

	// Now delete key1 via PrefixDelete operation
	_, err = db.Update([]BatchOp{PrefixDelete(key1)})
	r.NoError(err, "Delete key1 with PrefixDelete")

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

	_, err = db.Update([]BatchOp{
		Put(key3, value3),
		Put(key4, emptyValue4),
	})
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

// TestDeleteKeyWithChildren verifies that deleting a key that has children (keys
// that are extensions of it) does not delete the children. This is to ensure
// Delete doesn't do prefix delete.
func TestDeleteKeyWithChildren(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys := [][]byte{[]byte("parent"), []byte("parent/child1"), []byte("parent/child2")}
	values := [][]byte{[]byte("parent-value"), []byte("child1-value"), []byte("child2-value")}
	batch := makeBatch(keys, values)
	_, err := db.Update(batch)
	r.NoError(err, "Insert parent and children")

	for _, key := range keys {
		got, err := db.Get(key)
		r.NoError(err)
		r.NotNil(got, "key %s should exist", key)
	}

	// Delete only the parent key (not using PrefixDelete which would delete all)
	_, err = db.Update([]BatchOp{Delete(keys[0])})
	r.NoError(err, "Delete parent key")

	// Verify parent is deleted
	got, err := db.Get(keys[0])
	r.NoError(err)
	r.Nil(got, "parent key should be deleted")

	// Verify all children still exist
	for _, key := range keys[1:] {
		got, err := db.Get(key)
		r.NoError(err)
		r.NotNil(got, "key %s should still exist after parent deletion", key)
	}
}

// TestCloseWithCancelledContext verifies that Database.Close returns
// ErrActiveKeepAliveHandles when the context is cancelled before handles are dropped.
func TestCloseWithCancelledContext(t *testing.T) {
	r := require.New(t)
	db, err := newDatabase(t.TempDir())
	r.NoError(err)

	// Create a proposal to keep a handle active
	_, _, batch := kvForTest(1)
	proposal, err := db.Propose(batch)
	r.NoError(err)

	ctx, cancel := context.WithCancel(t.Context())

	var wg sync.WaitGroup
	wg.Go(func() {
		err = db.Close(ctx)
	})

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
	db, err := newDatabase(t.TempDir())
	r.NoError(err)

	// Create multiple proposals
	_, _, batch := kvForTest(3)
	proposal1, err := db.Propose(batch[:1])
	r.NoError(err)
	proposal2, err := db.Propose(batch[1:2])
	r.NoError(err)
	proposal3, err := db.Propose(batch[2:3])
	r.NoError(err)

	// Create multiple revisions
	root1, err := db.Update([]BatchOp{Put(keyForTest(10), valForTest(10))})
	r.NoError(err)
	root2, err := db.Update([]BatchOp{Put(keyForTest(20), valForTest(20))})
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
	db, err := newDatabase(t.TempDir())
	r.NoError(err)

	// Create two active proposals
	_, _, batch := kvForTest(2)
	proposal1, err := db.Propose(batch[:1])
	r.NoError(err)
	proposal2, err := db.Propose(batch[1:2])
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

func TestDump(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	// Test dump on empty database
	dump, err := db.Dump()
	r.NoError(err)
	r.Contains(dump, "digraph", "dump should be in DOT format")

	// Insert a few keys and values
	keys := [][]byte{[]byte("key1"), []byte("key2"), []byte("key3")}
	vals := [][]byte{[]byte("value1"), []byte("value2"), []byte("value3")}
	root, err := db.Update(makeBatch(keys, vals))
	r.NoError(err)
	r.NotNil(root)

	// Test dump on database with data
	dump, err = db.Dump()
	r.NoError(err)
	r.Contains(dump, "digraph", "dump should be in DOT format")
	// Verify that the values appear in the dump (keys are encoded/abbreviated in DOT format)
	for _, val := range vals {
		r.Contains(dump, string(val), "dump should contain value: %s", string(val))
	}

	// Test dump on revision
	revision, err := db.Revision(root)
	r.NoError(err)
	defer func() {
		r.NoError(revision.Drop())
	}()

	revisionDump, err := revision.Dump()
	r.NoError(err)
	r.Contains(revisionDump, "digraph", "revision dump should be in DOT format")
	for _, val := range vals {
		r.Contains(revisionDump, string(val), "revision dump should contain value: %s", string(val))
	}

	// Test dump on proposal
	newKeys := [][]byte{[]byte("key4")}
	newVals := [][]byte{[]byte("value4")}
	proposal, err := db.Propose(makeBatch(newKeys, newVals))
	r.NoError(err)
	defer func() {
		r.NoError(proposal.Drop())
	}()

	proposalDump, err := proposal.Dump()
	r.NoError(err)
	r.Contains(proposalDump, "digraph", "proposal dump should be in DOT format")
	// Proposal should contain both old and new values
	for _, val := range append(vals, newVals...) {
		r.Contains(proposalDump, string(val), "proposal dump should contain value: %s", string(val))
	}
}

var block_307_items string = `
d20628954718b36081ad06b1a04fb8896f3090eb5c7ab3cd9086e50e61d88b7eb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6:944abef613822fb2031d897e792f89c896ddafc466
d20628954718b36081ad06b1a04fb8896f3090eb5c7ab3cd9086e50e61d88b7e:f8450180a00000000000000000000000000000000000000000000000000000000000000000a0a33ec8100b06cfbacc612bc2baa4d36f01d5d52df97bcef04f54d5b8ceccbd9280
f22bb46edf31af855938befaa870ed3d86a4ad93a9ecf7c63ceaa80daea9ac4d:f84e8089063443e4a44dc98c00a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0611445c10cb8404ad2103d510e139c3ace61b5acec80505bd1f5870528f587d780
71645710251b71938795615f63f7bcf27fd5aa4ffbaeb7ace16cbd1c76a41e42:f84d1188662cdf1b7d1bc000a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a47080
`

var block_308_items string = `
d20628954718b36081ad06b1a04fb8896f3090eb5c7ab3cd9086e50e61d88b7e290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563:944abef613822fb2031d897e792f89c896ddafc466
d20628954718b36081ad06b1a04fb8896f3090eb5c7ab3cd9086e50e61d88b7e:f8450180a00000000000000000000000000000000000000000000000000000000000000000a0a33ec8100b06cfbacc612bc2baa4d36f01d5d52df97bcef04f54d5b8ceccbd9280
f22bb46edf31af855938befaa870ed3d86a4ad93a9ecf7c63ceaa80daea9ac4d:f84e808906348c71086eb3ac00a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0611445c10cb8404ad2103d510e139c3ace61b5acec80505bd1f5870528f587d780
71645710251b71938795615f63f7bcf27fd5aa4ffbaeb7ace16cbd1c76a41e42:f84d128865e452b75c31a000a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a47080
`

func TestProposeOnProposalRehash(t *testing.T) {
	r := require.New(t)

	b307k, b307v, err := parseKVFromDumpFormat(block_307_items)
	r.NoError(err, "load 307")

	b308k, b308v, err := parseKVFromDumpFormat(block_308_items)
	r.NoError(err, "load 308")

	var normalRoot, proposeOnProposalRoot Hash

	{
		db := newTestDatabase(t)

		p1, err := db.Propose(makeBatch(b307k, b307v))
		r.NoError(err, "propose 307")
		r.NoError(p1.Commit(), "commit 307")

		p2, err := db.Propose(makeBatch(b308k, b308v))
		r.NoError(err, "propose 308")

		normalRoot = p2.Root()
		r.NoError(err, "root")
	}

	{
		db := newTestDatabase(t)

		p1, err := db.Propose(makeBatch(b307k, b307v))
		r.NoError(err, "propose 307")

		p2, err := p1.Propose(makeBatch(b308k, b308v))
		r.NoError(err, "propose#2 308")

		proposeOnProposalRoot = p2.Root()
		r.NoError(err, "root")
	}

	r.Equal(normalRoot, proposeOnProposalRoot)
}

func parseKVFromDumpFormat(dumpstr string) ([][]byte, [][]byte, error) {
	// TODO: make this same as dump format outputted by firewood (slightly different, this is a debug one from my replay stuff)
	keys := make([][]byte, 0)
	values := make([][]byte, 0)
	lines := strings.SplitSeq(dumpstr, "\n")
	for line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			return nil, nil, errors.New("invalid line format")
		}
		key, value := parts[0], parts[1]
		keyBytes, err := hex.DecodeString(key)
		if err != nil {
			return nil, nil, err
		}
		valueBytes, err := hex.DecodeString(value)
		if err != nil {
			return nil, nil, err
		}
		keys = append(keys, keyBytes)
		values = append(values, valueBytes)
	}
	return keys, values, nil
}
