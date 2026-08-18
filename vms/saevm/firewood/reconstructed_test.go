// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"testing"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// replayAddr is the account written by [applyBlock].
var replayAddr = common.HexToAddress("1234")

// applyBlock writes block num's state changes, which are chosen so that every
// block produces a distinct root.
func applyBlock(sdb *state.StateDB, num uint64) {
	sdb.SetNonce(replayAddr, num)
	sdb.SetBalance(replayAddr, uint256.NewInt(100*num))
	sdb.SetState(replayAddr, common.Hash{byte(num)}, common.Hash{byte(num)})
}

// commitSeed applies and commits block 1, returning a root with a committed
// revision that can seed a reconstructed view.
func commitSeed(t *testing.T, db state.Database) common.Hash {
	t.Helper()

	sdb := newStateDB(t, db, types.EmptyRootHash)
	applyBlock(sdb, 1)
	root, err := sdb.Commit(1, true /* EIP-158 */)
	require.NoError(t, err, "state.StateDB.Commit() creating seed")
	require.NoErrorf(t, db.TrieDB().Commit(root, false), "triedb.Commit(%s)", root)
	return root
}

// newReconstructedDB returns a [state.Database] backed by an isolated
// reconstructed view of db seeded at root, along with its release function. The
// view is also released when the test ends.
func newReconstructedDB(t *testing.T, db state.Database, root common.Hash) (state.Database, func()) {
	t.Helper()

	tdb, ok := db.TrieDB().Backend().(*TrieDB)
	require.Truef(t, ok, "triedb.Database.Backend() is %T, not %T", db.TrieDB().Backend(), tdb)
	recon, err := tdb.NewReconstructed(root)
	require.NoErrorf(t, err, "%T.NewReconstructed(%s)", tdb, root)

	reconDB, release := NewReconstructedDatabase(db, tdb, recon, root)
	t.Cleanup(release)
	return reconDB, release
}

// newReconstructedStateDB returns a [state.StateDB] over a reconstructed view of
// db seeded at root.
func newReconstructedStateDB(t *testing.T, db state.Database, root common.Hash) *state.StateDB {
	t.Helper()

	reconDB, _ := newReconstructedDB(t, db, root)
	sdb, err := state.New(root, reconDB, nil /* snapshots */)
	require.NoErrorf(t, err, "state.New(%s) on reconstructed view", root)
	return sdb
}

// TestReconstructedRootsMatchCanonical applies the same state changes to a
// canonical Firewood state and to a reconstructed view seeded from a committed
// revision, requiring that both produce the same roots.
func TestReconstructedRootsMatchCanonical(t *testing.T) {
	slot := common.Hash{0x1}

	tests := []struct {
		name string
		run  func(sdb *state.StateDB, root func())
	}{
		{
			name: "blocks_finalised_between_roots",
			run: func(sdb *state.StateDB, root func()) {
				for num := uint64(2); num <= 4; num++ {
					applyBlock(sdb, num)
					sdb.Finalise(true /* EIP-158 */)
				}
				root()
			},
		},
		{
			name: "root_after_every_transaction",
			run: func(sdb *state.StateDB, root func()) {
				for i := uint64(1); i <= 3; i++ {
					sdb.SetNonce(replayAddr, i)
					sdb.SetBalance(replayAddr, uint256.NewInt(7*i))
					sdb.SetState(replayAddr, common.Hash{byte(i)}, common.Hash{0xff})
					root()
				}
			},
		},
		{
			name: "selfdestruct_then_recreate",
			run: func(sdb *state.StateDB, root func()) {
				sdb.CreateAccount(replayAddr)
				sdb.SetNonce(replayAddr, 1)
				sdb.SetBalance(replayAddr, uint256.NewInt(1))
				sdb.SetState(replayAddr, slot, common.Hash{0xaa})
				sdb.Selfdestruct6780(replayAddr) // eligible: created this transaction
				sdb.Finalise(true /* EIP-158 */)

				sdb.CreateAccount(replayAddr)
				sdb.SetNonce(replayAddr, 2)
				sdb.SetBalance(replayAddr, uint256.NewInt(2))
				sdb.SetState(replayAddr, slot, common.Hash{0xbb})
				root()

				require.Equal(t, common.Hash{0xbb}, sdb.GetState(replayAddr, slot), "storage of the recreated account")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDB(t)
			seed := commitSeed(t, db)

			var want []common.Hash
			canonical := newStateDB(t, db, seed)
			tt.run(canonical, func() {
				want = append(want, canonical.IntermediateRoot(true /* EIP-158 */))
			})
			var got []common.Hash
			reconstructed := newReconstructedStateDB(t, db, seed)
			tt.run(reconstructed, func() {
				got = append(got, reconstructed.IntermediateRoot(true /* EIP-158 */))
			})
			require.Equal(t, want, got, "roots of the reconstructed view")
		})
	}
}

// TestReconstructedRejectsCanonicalOperations verifies that a reconstructed
// state refuses the operations that would let it serve or persist canonical
// state.
func TestReconstructedRejectsCanonicalOperations(t *testing.T) {
	db := newDB(t)
	seed := commitSeed(t, db)

	reconDB, _ := newReconstructedDB(t, db, seed)
	_, err := reconDB.OpenTrie(common.Hash{0xaa}) // any root other than the view's
	require.ErrorIs(t, err, errUnexpectedReconstructedRoot, "state.Database.OpenTrie() at another root")

	sdb := newReconstructedStateDB(t, db, seed)
	applyBlock(sdb, 2)
	contract := common.Address{0xcc}
	code := []byte{0x60, 0x00}
	sdb.SetCode(contract, code)
	codeHash := sdb.GetCodeHash(contract)
	require.False(t, rawdb.HasCode(db.DiskDB(), codeHash), "canonical database has code before Commit()")
	_, err = sdb.Commit(2, true /* EIP-158 */)
	require.ErrorIs(t, err, errCommitReconstructedState, "state.StateDB.Commit() on reconstructed view")
	require.False(t, rawdb.HasCode(db.DiskDB(), codeHash), "canonical database has code after Commit()")
}

// TestReconstructedCopyIsIndependent verifies that writes through a copied state
// do not reach the original's native view, nor the canonical code database.
func TestReconstructedCopyIsIndependent(t *testing.T) {
	const nonceAfterCopy = 7

	db := newDB(t)
	seed := commitSeed(t, db)

	// The root canonical execution produces for the change made to the original
	// state after its copy diverges. Writes leaking from the copy's view into
	// the original's would change the original's root.
	canonical := newStateDB(t, db, seed)
	canonical.SetNonce(replayAddr, nonceAfterCopy)
	want := canonical.IntermediateRoot(true /* EIP-158 */)

	original := newReconstructedStateDB(t, db, seed)
	copied := original.Copy()

	var (
		contract = common.Address{0xcc}
		code     = []byte{0x60, 0x00}
	)
	copied.SetCode(contract, code)
	codeHash := copied.GetCodeHash(contract)
	copied.Finalise(true /* EIP-158 */)
	copiedRoot := copied.IntermediateRoot(true /* EIP-158 */) // advances the copy's view
	require.Equal(t, code, copied.GetCode(contract), "code created in the copied state")
	require.Empty(t, original.GetCode(contract), "code in the original state after finalizing the copy")
	require.Empty(t, rawdb.ReadCode(db.DiskDB(), codeHash), "code written to the canonical database")

	original.SetNonce(replayAddr, nonceAfterCopy)
	got := original.IntermediateRoot(true /* EIP-158 */)
	require.Equal(t, want, got, "original root after mutating the copy")
	require.NotEqual(t, want, copiedRoot, "copied root after mutation")
}

// TestReconstructedCopyTrieCarriesPendingOperations verifies that a trie copied
// before its operations reach the native view applies them to its own clone.
// [state.StateDB] hashes before copying, so only direct trie users reach this.
func TestReconstructedCopyTrieCarriesPendingOperations(t *testing.T) {
	db := newDB(t)
	seed := commitSeed(t, db)

	reconDB, _ := newReconstructedDB(t, db, seed)
	tr, err := reconDB.OpenTrie(seed)
	require.NoErrorf(t, err, "%T.OpenTrie(%s)", reconDB, seed)

	account, err := tr.GetAccount(replayAddr)
	require.NoErrorf(t, err, "%T.GetAccount(%s)", tr, replayAddr)
	require.NotNilf(t, account, "%T.GetAccount(%s) account", tr, replayAddr)
	account.Nonce++
	require.NoErrorf(t, tr.UpdateAccount(replayAddr, account), "%T.UpdateAccount(%s)", tr, replayAddr)

	copied := reconDB.CopyTrie(tr)
	require.NotNil(t, copied, "state.Database.CopyTrie()")

	want := tr.Hash()
	require.NotEqual(t, common.Hash{}, want, "original trie root with pending operations")
	require.Equal(t, want, copied.Hash(), "copied trie root with pending operations")
}

// TestReconstructedReleaseDropsEveryView verifies that the release function owns
// the original view and every clone, and that it is idempotent.
func TestReconstructedReleaseDropsEveryView(t *testing.T) {
	db := newDB(t)
	seed := commitSeed(t, db)

	reconDB, release := newReconstructedDB(t, db, seed)
	original, err := state.New(seed, reconDB, nil /* snapshots */)
	require.NoErrorf(t, err, "state.New(%s) on reconstructed view", seed)
	copied := original.Copy()

	release()
	release()

	untouched := common.Address{0xdd}
	_ = original.GetNonce(untouched)
	_ = copied.GetNonce(untouched)
	require.ErrorIs(t, original.Error(), ffi.ErrDroppedReconstructed, "original StateDB read after release")
	require.ErrorIs(t, copied.Error(), ffi.ErrDroppedReconstructed, "copied StateDB read after release")
}
