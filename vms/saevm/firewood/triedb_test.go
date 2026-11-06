// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"math"
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/rapidtest"
	"github.com/ava-labs/avalanchego/utils/set"
)

func newDB(t testing.TB) state.Database {
	t.Helper()

	cfg := DefaultConfig(t.TempDir(), loggingtest.New(t, logging.Debug))
	db := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), &triedb.Config{
		DBOverride: cfg.BackendConstructor,
	})
	t.Cleanup(func() {
		assert.NoError(t, db.TrieDB().Close(), "triedb.Close()")
	})
	return db
}

func newStateDB(t testing.TB, db state.Database, root common.Hash) *state.StateDB {
	t.Helper()

	sdb, err := state.New(root, db, nil)
	require.NoErrorf(t, err, "state.New(%s, %T)", root, db)
	t.Cleanup(func() {
		assert.NoErrorf(t, sdb.Error(), "%T.Error()", sdb)
	})

	return sdb
}

// A backend is a stateful wrapper around a statedb.
type backend struct {
	db  state.Database
	sdb *state.StateDB
}

func newBackend(t testing.TB, db state.Database) *backend {
	return &backend{
		db:  db,
		sdb: newStateDB(t, db, types.EmptyRootHash),
	}
}

// commit ends the current block, reopens a fresh StateDB at the new root and
// returns that root.
func (b *backend) commit(t testing.TB, blockNum uint64) common.Hash {
	t.Helper()
	root, err := b.sdb.Commit(blockNum, true /* EIP-158 */)
	require.NoErrorf(t, err, "%T.Commit()", b.sdb)
	b.sdb = newStateDB(t, b.db, root)
	return root
}

// copy replaces the statedb with a copy of itself.
func (b *backend) copy() {
	b.sdb = b.sdb.Copy()
}

// account is the in-memory reference for a single account.
type account struct {
	nonce       uint64
	balance     *uint256.Int
	activeSlots []common.Hash
}

type SUT struct {
	want, got *backend

	lastRoot common.Hash
	blockNum uint64

	accounts map[common.Address]*account
	addrs    []common.Address

	// Per-transaction tracking, reset at every transaction boundary
	// see [SUT.finaliseTx])
	createdThisTx    set.Set[common.Address] // eligible for selfdestruct6780
	destructedThisTx set.Set[common.Address] // eligible for resurrection
}

func newSUT(want, got *backend) *SUT {
	return &SUT{
		want:             want,
		got:              got,
		lastRoot:         types.EmptyRootHash,
		accounts:         make(map[common.Address]*account),
		createdThisTx:    set.NewSet[common.Address](0),
		destructedThisTx: set.NewSet[common.Address](0),
	}
}

func hash(b byte) common.Hash {
	return crypto.Keccak256Hash([]byte{b})
}

// createAccount creates an account, modelling an EVM contract deployment. When
// an account has been destructed earlier in the current transaction, that
// accound could be resurrected.
//
// The created account is EIP-168 empty, to encourage collisions in journal
// handling and set states of deleted accounts.
// To guarantee an account is persisted, one must also call
// [SUT.updateAccount] or [SUT.setStorage] on the account.
func (s *SUT) createAccount(tb testing.TB, addr common.Address) {
	if _, ok := s.accounts[addr]; ok {
		tb.Logf("skipping account creation (addr=%s) because it already exists", addr)
		return
	}
	if s.destructedThisTx.Contains(addr) {
		// The account was destructed earlier this tx, so this is a
		// resurrection.
		tb.Logf("resurrecting destructed account (addr=%s)", addr)
		s.destructedThisTx.Remove(addr)
	} else {
		tb.Logf("creating new account (addr=%s)", addr)
	}

	s.accounts[addr] = &account{
		nonce:   0,
		balance: uint256.NewInt(0),
	}
	s.addrs = append(s.addrs, addr)
	s.createdThisTx.Add(addr)

	s.want.sdb.CreateAccount(addr)
	s.got.sdb.CreateAccount(addr)
}

func (s *SUT) updateAccount(tb testing.TB, addr common.Address) {
	tb.Logf("updating account (addr=%s)", addr)
	acc := s.accounts[addr]
	acc.nonce++
	acc.balance = new(uint256.Int).Add(acc.balance, uint256.NewInt(1))

	s.want.sdb.SetNonce(addr, acc.nonce)
	s.want.sdb.SetBalance(addr, acc.balance)
	s.got.sdb.SetNonce(addr, acc.nonce)
	s.got.sdb.SetBalance(addr, acc.balance)
}

// selfDestruct6780 models the SELFDESTRUCT opcode under EIP-6780.
//
// The call is always issued to the StateDB (it is a legal operation on any
// account), but it only actually destructs the account — and so only updates
// the model — when the account was created in the current transaction. On a
// pre-existing account it is a no-op and the account remains live.
func (s *SUT) selfDestruct6780(addr common.Address) {
	s.want.sdb.Selfdestruct6780(addr)
	s.got.sdb.Selfdestruct6780(addr)

	if !s.createdThisTx.Contains(addr) {
		return // not created this tx: Selfdestruct6780 is a no-op
	}

	delete(s.accounts, addr)
	s.createdThisTx.Remove(addr)
	s.destructedThisTx.Add(addr)
	s.addrs = utils.DeleteIndex(s.addrs, slices.Index(s.addrs, addr))
}

func (s *SUT) setStorage(tb testing.TB, addr common.Address, k, v byte) {
	tb.Logf("setting storage for %s", addr)
	key, val := hash(k), hash(v)
	acc := s.accounts[addr]
	acc.activeSlots = append(acc.activeSlots, key)

	// To prevent cyclical roots (A -> B -> A), update nonce
	acc.nonce++
	s.want.sdb.SetState(addr, key, val)
	s.want.sdb.SetNonce(addr, acc.nonce)
	s.got.sdb.SetState(addr, key, val)
	s.got.sdb.SetNonce(addr, acc.nonce)
}

// deleteStorage zeroes accounts[addr].activeSlots[slot], which must be in range.
func (s *SUT) deleteStorage(addr common.Address, slot int) {
	acc := s.accounts[addr]
	key := acc.activeSlots[slot]
	acc.activeSlots = utils.DeleteIndex(acc.activeSlots, slot)

	// To prevent cyclical roots (A -> B -> A), update nonce
	acc.nonce++
	s.want.sdb.SetState(addr, key, common.Hash{})
	s.want.sdb.SetNonce(addr, acc.nonce)
	s.got.sdb.SetState(addr, key, common.Hash{})
	s.got.sdb.SetNonce(addr, acc.nonce)
}

// read compares account fields and, if slot >= 0, the storage slot
// accounts[addr].activeSlots[slot].
func (s *SUT) read(tb testing.TB, addr common.Address, slot int) {
	want, got := s.want.sdb, s.got.sdb
	require.Equal(tb, want.GetNonce(addr), got.GetNonce(addr), "nonce mismatch for %s", addr)
	require.Equal(tb, want.Exist(addr), got.Exist(addr), "existence mismatch for %s", addr)
	require.Equal(tb, want.Empty(addr), got.Empty(addr), "emptiness mismatch for %s", addr)
	require.Equal(tb, want.GetBalance(addr), got.GetBalance(addr), "balance mismatch for %s", addr)
	// [state.StateDB.GetStorageRoot] is known not to produce compatible
	// outputs.
	require.Equal(tb, want.GetCode(addr), got.GetCode(addr), "code mismatch for %s", addr)
	require.Equal(tb, want.GetCodeSize(addr), got.GetCodeSize(addr), "code size mismatch for %s", addr)
	require.Equal(tb, want.GetCodeHash(addr), got.GetCodeHash(addr), "code hash mismatch for %s", addr)
	require.Equal(tb, want.HasSelfDestructed(addr), got.HasSelfDestructed(addr), "self-destruct mismatch for %s", addr)

	if slot < 0 {
		return
	}
	key := s.accounts[addr].activeSlots[slot]

	require.Equal(tb, want.GetState(addr, key), got.GetState(addr, key), "storage mismatch for %s[%s]", addr, key)
	require.Equal(tb, want.GetCommittedState(addr, key), got.GetCommittedState(addr, key), "committed storage mismatch for %s[%s]", addr, key)
}

// finaliseTx resets the model's per-transaction tracking to mirror
// [state.StateDB.Finalise].
// Any accounts created this transaction can no longer be selfdestructed,
// and any accounts destructed this transaction can no longer be resurrected.
func (s *SUT) finaliseTx() {
	s.createdThisTx.Clear()
	s.destructedThisTx.Clear()
}

func (s *SUT) finalise() {
	s.want.sdb.Finalise(true /* EIP-158 */)
	s.got.sdb.Finalise(true /* EIP-158 */)
	s.finaliseTx()
}

func (s *SUT) intermediateRoot(t testing.TB) {
	want := s.want.sdb.IntermediateRoot(true /* EIP-158 */)
	got := s.got.sdb.IntermediateRoot(true /* EIP-158 */)
	require.Equal(t, want, got, "root mismatch")
	s.finaliseTx() // IntermediateRoot calls Finalise
}

func (s *SUT) stateDBCommit(t testing.TB) {
	want := s.want.commit(t, s.blockNum)
	got := s.got.commit(t, s.blockNum)
	require.Equal(t, want, got, "root mismatch after commit")

	s.lastRoot = want
	s.blockNum++
	s.finaliseTx() // Commit calls Finalise
}

func (s *SUT) diskCommit(t testing.TB) {
	want, got := s.want.db, s.got.db
	require.NoErrorf(t, want.TrieDB().Commit(s.lastRoot, false), "%T.TrieDB().Commit(%s)", want, s.lastRoot)
	require.NoErrorf(t, got.TrieDB().Commit(s.lastRoot, false), "%T.TrieDB().Commit(%s)", got, s.lastRoot)
}

func (s *SUT) copyStateDB() {
	// The StateDB copy is NOT safe to be done in the middle of a transaction.
	// The journal isn't copied, so any "touches" (EIP-168 empty accounts with
	// storage) are lost, so the account will NOT be deleted as expected.
	s.finalise()

	s.want.copy()
	s.got.copy()
}

// drawAddr draws an existing account.
func (s *SUT) drawAddr(rt *rapid.T) common.Address {
	if len(s.addrs) == 0 {
		rt.Skip("no accounts")
	}
	return rapid.SampledFrom(s.addrs).Draw(rt, "addr")
}

// drawDestructed draws an account selfdestructed in the current transaction.
func (s *SUT) drawDestructed(rt *rapid.T) common.Address {
	if s.destructedThisTx.Len() == 0 {
		rt.Skip("nothing destructed this tx")
	}
	destructed := s.destructedThisTx.List()
	slices.SortFunc(destructed, common.Address.Cmp)
	return rapid.SampledFrom(destructed).Draw(rt, "addr")
}

// drawAddrWithSlots draws an account with at least one active slot.
func (s *SUT) drawAddrWithSlots(rt *rapid.T) common.Address {
	var withSlots []common.Address
	for _, a := range s.addrs {
		if len(s.accounts[a].activeSlots) > 0 {
			withSlots = append(withSlots, a)
		}
	}
	if len(withSlots) == 0 {
		rt.Skip("no slots")
	}
	return rapid.SampledFrom(withSlots).Draw(rt, "addr")
}

// drawSlot draws an index into addr's active slots, or -1 if it has none.
func (s *SUT) drawSlot(rt *rapid.T, addr common.Address) int {
	n := len(s.accounts[addr].activeSlots)
	if n == 0 {
		return -1
	}
	return rapid.IntRange(0, n-1).Draw(rt, "slot")
}

// Action names for [actions].
const (
	actCreateAccount    = "createAccount"
	actResurrectAccount = "resurrectAccount"
	actUpdateAccount    = "updateAccount"
	actSelfDestruct6780 = "selfDestruct6780"
	actSetStorage       = "setStorage"
	actDeleteStorage    = "deleteStorage"
	actRead             = "read"
	actFinalise         = "finalise"
	actIntermediateRoot = "intermediateRoot"
	actStateDBCommit    = "stateDBCommit"
	actDiskCommit       = "diskCommit"
	actCopyStateDB      = "copyStateDB"
)

// TODO(#5539): support [*state.StateDB.SelfDestruct]
func actions(tb testing.TB, s *SUT) map[string]func(*rapid.T) {
	// Small spaces so runs collide on the same accounts and slots.
	const numAddrs, numKeys = 16, 16
	addr := rapid.ByteMax(numAddrs - 1)
	key := rapid.ByteMax(numKeys - 1)
	return map[string]func(*rapid.T){
		actCreateAccount:    func(rt *rapid.T) { s.createAccount(tb, common.Address{addr.Draw(rt, "addr")}) },
		actResurrectAccount: func(rt *rapid.T) { s.createAccount(tb, s.drawDestructed(rt)) },
		actUpdateAccount:    func(rt *rapid.T) { s.updateAccount(tb, s.drawAddr(rt)) },
		actSelfDestruct6780: func(rt *rapid.T) { s.selfDestruct6780(s.drawAddr(rt)) },
		actSetStorage: func(rt *rapid.T) {
			s.setStorage(tb, s.drawAddr(rt), key.Draw(rt, "key"), key.Draw(rt, "val"))
		},
		actDeleteStorage: func(rt *rapid.T) {
			a := s.drawAddrWithSlots(rt)
			s.deleteStorage(a, s.drawSlot(rt, a))
		},
		actRead: func(rt *rapid.T) {
			a := s.drawAddr(rt)
			s.read(tb, a, s.drawSlot(rt, a))
		},
		actFinalise:         func(*rapid.T) { s.finalise() },
		actIntermediateRoot: func(*rapid.T) { s.intermediateRoot(tb) },
		actStateDBCommit:    func(*rapid.T) { s.stateDBCommit(tb) },
		actDiskCommit:       func(*rapid.T) { s.diskCommit(tb) },
		actCopyStateDB:      func(*rapid.T) { s.copyStateDB() },
	}
}

// stateRootProp compares the state root of arbitrary operation sequences on a
// Firewood-backed [state.StateDB] against a reference: HashDB, or when
// reconstructed, a Firewood StateDB opened past the tip.
func stateRootProp(tb testing.TB, reconstructed bool) func(*rapid.T) {
	return func(rt *rapid.T) {
		rtb := rapidtest.New(tb, rt)
		var s *SUT
		if reconstructed {
			s = newSUT(newBackend(rtb, newDB(rtb)), newReconstructedBackend(rtb))
		} else {
			s = newSUT(
				newBackend(rtb, state.NewDatabase(rawdb.NewMemoryDatabase())), // hashdb
				newBackend(rtb, newDB(rtb)),                                   // firewood
			)
		}
		acts := actions(rtb, s)
		if reconstructed {
			// Commits are not valid on a reconstructed state.
			delete(acts, actStateDBCommit)
			delete(acts, actDiskCommit)
		}
		rt.Repeat(acts)
		s.intermediateRoot(rtb) // final flush to tries and verify all pending state
	}
}

func TestStateRoot(t *testing.T) {
	rapid.Check(t, stateRootProp(t, false))
}

func TestReconstructedRoot(t *testing.T) {
	rapid.Check(t, stateRootProp(t, true))
}

func FuzzStateRoot(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		rapid.MakeFuzz(stateRootProp(t, false))(t, b)
	})
}

func FuzzReconstructedRoot(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		rapid.MakeFuzz(stateRootProp(t, true))(t, b)
	})
}

// newReconstructedBackend returns a Firewood backend whose StateDB is opened at
// the empty root after the tip has moved past it. One MUST NOT call
// [state.StateDB.Commit] or [triedb.Database.Commit] on it, as these are not
// eligible operations.
func newReconstructedBackend(t testing.TB) *backend {
	db := newDB(t)

	// The sdb address is never generated by the model
	sdb := newStateDB(t, db, types.EmptyRootHash)
	sdb.SetNonce(common.Address{0xff}, 1)
	fillerRoot, err := sdb.Commit(0, true /* EIP-158 */)
	require.NoError(t, err, "filler Commit()")
	require.NoErrorf(t, db.TrieDB().Commit(fillerRoot, false), "triedb.Commit(%s)", fillerRoot)

	return newBackend(t, db)
}

func TestGenesis(t *testing.T) {
	cfg := DefaultConfig(t.TempDir(), loggingtest.New(t, logging.Debug))
	memDB := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memDB, &triedb.Config{
		DBOverride: cfg.BackendConstructor,
	})

	g := core.Genesis{
		Config: params.MergedTestChainConfig,
		Alloc: types.GenesisAlloc{
			common.BytesToAddress([]byte{1}): {Balance: big.NewInt(100)},
		},
		Timestamp:  0,
		Difficulty: big.NewInt(0), // irrelevant but required
	}

	genesisRoot := g.ToBlock().Root()
	require.False(t, tdb.Initialized(genesisRoot), "Genesis root should not be initialized in the database")

	_, _, err := core.SetupGenesisBlock(memDB, tdb, &g)
	require.NoError(t, err, "SetupGenesisBlock()")
	require.True(t, tdb.Initialized(genesisRoot), "Genesis root should be initialized in the database after setup")
	require.NoError(t, tdb.Close(), "triedb.Close()")

	t.Run("recovery", func(t *testing.T) {
		cfg.Log = loggingtest.New(t, logging.Debug)
		tdb = triedb.NewDatabase(memDB, &triedb.Config{
			DBOverride: cfg.BackendConstructor,
		})
		require.True(t, tdb.Initialized(genesisRoot), "Genesis root should still be initialized in the database")
		require.NoError(t, tdb.Close(), "triedb.Close()")
	})
}

// TestMultipleProposals verifies that a single [triedb.Commit] call
// chains the commit of dependent proposals.
func TestMultipleProposals(t *testing.T) {
	cfg := DefaultConfig(t.TempDir(), loggingtest.New(t, logging.Debug))
	db := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), &triedb.Config{
		DBOverride: cfg.BackendConstructor,
	})

	const numBlocks = 5
	lastRoot := types.EmptyRootHash
	for i := range numBlocks {
		addr := common.BytesToAddress([]byte{byte(i)})
		sdb := newStateDB(t, db, lastRoot)
		sdb.CreateAccount(addr)
		sdb.SetNonce(addr, 1)
		sdb.SetBalance(addr, uint256.NewInt(0))
		root, err := sdb.Commit(uint64(i), true)
		require.NoError(t, err, "sdb.Commit()")
		lastRoot = root
	}

	require.NoErrorf(t, db.TrieDB().Commit(lastRoot, false), "triedb.Commit(%s)", lastRoot)

	// Firewood loses all uncommitted proposals on close, to test that it was
	// committed, we can close the database
	require.NoErrorf(t, db.TrieDB().Close(), "triedb.Close()")
	db = state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), &triedb.Config{
		DBOverride: cfg.BackendConstructor,
	})
	defer func() {
		assert.NoError(t, db.TrieDB().Close(), "triedb.Close()")
	}()

	// Would fail if an [ffi.Revision] cannot be found
	_, err := db.OpenTrie(lastRoot)
	require.NoErrorf(t, err, "%T.OpenTrie(%s)", db, lastRoot)
}

func TestInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func(Config) Config
		wantErr error
	}{
		{
			name: "empty_path",
			cfg: func(cfg Config) Config {
				cfg.Path = ""
				return cfg
			},
			wantErr: errPathNotProvided,
		},
		{
			name: "too_few_revisions",
			cfg: func(cfg Config) Config {
				cfg.StateHistory = 1 // Firewood requires >= 2
				return cfg
			},
			wantErr: nil, // floors to DeferredCommitInterval + 1
		},
		{
			name: "commit_interval_too_big",
			cfg: func(cfg Config) Config {
				cfg.DeferredCommitInterval = math.MaxUint64
				return cfg
			},
			wantErr: errCommitIntervalTooBig,
		},
		{
			name: "no_logger",
			cfg: func(cfg Config) Config {
				cfg.Log = nil
				return cfg
			},
			wantErr: errNoLogger,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg(DefaultConfig(t.TempDir(), loggingtest.New(t, logging.Debug)))
			_, err := New(cfg)
			require.ErrorIs(t, err, tt.wantErr, "New()")
		})
	}
}

func TestNoLoggerPanicsInBackendConstructor(t *testing.T) {
	cfg := DefaultConfig(t.TempDir(), nil)
	require.Panicsf(t, func() {
		_ = cfg.BackendConstructor(rawdb.NewMemoryDatabase())
	}, "%T.BackendConstructor()", cfg)
}

// TestUnknownCommitNoError verifies that committing a root that is not known to
// the TrieDB does not error. This is required for recovery in the VM.
func TestUnknownCommitNoError(t *testing.T) {
	db := newDB(t)
	root := common.Hash{0x1}
	require.NoErrorf(t, db.TrieDB().Commit(root, false), "triedb.Commit(%s)", root)
}

var (
	addr1 = common.Address{1}
	addr2 = common.Address{2}
	addr3 = common.Address{3}
)

// TestCommitOnlyOneOfTwoProposals verifies that when two StateDBs branch from
// the same parent, only the first is accepted by the TrieDB.
func TestCommitOnlyOneOfTwoProposals(t *testing.T) {
	db := newDB(t)
	tip := types.EmptyRootHash

	a := newStateDB(t, db, tip)
	b := newStateDB(t, db, tip)
	a.SetNonce(addr1, 1)
	b.SetNonce(addr2, 1)
	rootA := a.IntermediateRoot(true)
	rootB := b.IntermediateRoot(true)
	require.NotEqual(t, rootA, rootB, "different changes yield different roots")

	got, err := a.Commit(1, true)
	require.NoError(t, err, "a.Commit()")
	require.Equal(t, rootA, got, "a.Commit() root")

	// b's proposal was valid when hashed, but the TrieDB rejects a second
	// proposal on the same parent.
	_, err = b.Commit(1, true)
	require.ErrorIs(t, err, errParentNotLatest, "b.Commit()")

	tdb := db.TrieDB()
	require.NoErrorf(t, tdb.Commit(rootA, false), "triedb.Commit(%s)", rootA)
	require.NoErrorf(t, tdb.Commit(rootB, false), "triedb.Commit(%s) of unknown root", rootB)

	// We CANNOT guarantee that the rootB isn't available, since it depends on the GC.
	_, err = db.OpenTrie(rootA)
	require.NoErrorf(t, err, "db.OpenTrie(%s)", rootA)
}

// TestTrieCommitRejectsLostProposal verifies that a proposal handed to the
// TrieDB by a failed commit is not silently overwritten by a later one.
func TestTrieCommitRejectsLostProposal(t *testing.T) {
	db := newDB(t)
	tdb := db.TrieDB()
	tip := types.EmptyRootHash

	a := newStateDB(t, db, tip)
	b := newStateDB(t, db, tip)
	a.SetNonce(addr1, 1)
	b.SetNonce(addr2, 1)
	b.IntermediateRoot(true) // proposal created while the parent is still the tip
	rootA, err := a.Commit(1, true)
	require.NoError(t, err, "a.Commit()")
	require.NoErrorf(t, tdb.Commit(rootA, false), "triedb.Commit(%s)", rootA)

	// b's proposal is handed to the TrieDB before [TrieDB.Update] rejects it,
	// so it remains pending.
	_, err = b.Commit(1, true)
	require.ErrorIs(t, err, errParentNotLatest, "b.Commit()")

	c := newStateDB(t, db, rootA)
	c.SetNonce(addr3, 1)
	_, err = c.Commit(2, true)
	require.ErrorIs(t, err, errProposalPending, "c.Commit() while b's proposal is pending")
}

// TestProposalToReconstruction creates a proposal that ends up being
// uncommittable. To allow hashing again, it replaces the proposal with a
// reconstruction internally.
func TestProposalToReconstruction(t *testing.T) {
	db := newDB(t)
	tip := types.EmptyRootHash

	a := newStateDB(t, db, tip)
	b := newStateDB(t, db, tip)
	a.SetNonce(addr1, 1)
	b.SetNonce(addr2, 1)
	b.IntermediateRoot(true) // creates proposal

	// Reference for b's eventual state, proposed while tip is still proposable.
	want := newStateDB(t, db, tip)
	want.SetNonce(addr2, 1)
	want.SetNonce(addr3, 1)
	wantRoot := want.IntermediateRoot(true)

	rootA, err := a.Commit(1, true)
	require.NoError(t, err, "a.Commit()")
	require.NoErrorf(t, db.TrieDB().Commit(rootA, false), "triedb.Commit(%s)", rootA)

	// b should now match want
	// Hashing must now create a reconstruction instead of a proposal.
	b.SetNonce(addr3, 1)
	require.Equal(t, wantRoot, b.IntermediateRoot(true), "root via reconstruction")
	require.Equal(t, uint64(1), b.GetNonce(addr3), "GetNonce() after reconstruction")

	_, err = b.Commit(1, true)
	require.ErrorIs(t, err, errHistoricalNotCommittable, "b.Commit()")
}

// TestReconstructionFromHistoricalRevision verifies that a StateDB opened at a
// historical root can be repeatedly modified and hashed but never committed.
func TestReconstructionFromHistoricalRevision(t *testing.T) {
	db := newDB(t)
	tdb := db.TrieDB()

	blk1 := newStateDB(t, db, types.EmptyRootHash)
	blk1.SetNonce(addr1, 1)
	root1, err := blk1.Commit(1, true)
	require.NoError(t, err, "blk1.Commit()")
	require.NoErrorf(t, tdb.Commit(root1, false), "triedb.Commit(%s)", root1)

	// Reference for the final state, proposed while root1 is still the tip.
	addrs := []common.Address{{0xa}, {0xb}, {0xc}}
	want := newStateDB(t, db, root1)
	for _, addr := range addrs {
		want.SetNonce(addr, 1)
	}
	wantRoot := want.IntermediateRoot(true)

	blk2 := newStateDB(t, db, root1)
	blk2.SetNonce(addr2, 1)
	root2, err := blk2.Commit(2, true)
	require.NoError(t, err, "blk2.Commit()")
	require.NoErrorf(t, tdb.Commit(root2, false), "triedb.Commit(%s)", root2)

	sdb := newStateDB(t, db, root1)
	require.Equal(t, root1, sdb.IntermediateRoot(true), "IntermediateRoot() without changes")
	require.Equal(t, uint64(0), sdb.GetNonce(addr2), "block 2 is not visible at root1")

	prev := root1
	for i, addr := range addrs {
		sdb.SetNonce(addr, 1)
		root := sdb.IntermediateRoot(true)
		require.NotEqual(t, prev, root, "IntermediateRoot() after change %d", i)
		prev = root
	}
	require.Equal(t, wantRoot, prev, "final root matches the proposal reference")
	require.Equal(t, prev, sdb.IntermediateRoot(true), "IntermediateRoot() without new changes")

	_, err = sdb.Commit(2, true)
	require.ErrorIs(t, err, errHistoricalNotCommittable, "Commit()")
}

// TestCopyDoesNotAliasProposal verifies that a copy of a proposal-backed
// StateDB hashes independently and survives the original being committed.
func TestCopyDoesNotAliasProposal(t *testing.T) {
	db := newDB(t)
	tdb := db.TrieDB()

	base := newStateDB(t, db, types.EmptyRootHash)
	base.SetNonce(addr1, 1)
	tip, err := base.Commit(1, true)
	require.NoError(t, err, "base.Commit()")
	require.NoErrorf(t, tdb.Commit(tip, false), "triedb.Commit(%s)", tip)

	a := newStateDB(t, db, tip)
	a.SetNonce(addr1, 2)
	rootA := a.IntermediateRoot(true)

	cp := a.Copy()
	require.Equal(t, rootA, cp.IntermediateRoot(true), "copy hashes to the same root")

	_, err = a.Commit(2, true)
	require.NoError(t, err, "a.Commit()")
	require.NoErrorf(t, tdb.Commit(rootA, false), "triedb.Commit(%s)", rootA)
	require.Equal(t, uint64(2), cp.GetNonce(addr1), "GetNonce() on copy after original committed")
	require.Equal(t, rootA, cp.IntermediateRoot(true), "IntermediateRoot() on copy after original committed")

	// The copy holds its own proposal on tip, which is no longer the latest
	// root, so it is rejected rather than treated as the original's proposal.
	_, err = cp.Commit(2, true)
	require.ErrorIs(t, err, errParentNotLatest, "cp.Commit()")
}

// TestCopyClonesReconstruction verifies that a copy of a reconstruction-backed
// StateDB diverges independently of the original.
func TestCopyClonesReconstruction(t *testing.T) {
	db := newDB(t)
	tdb := db.TrieDB()

	blk1 := newStateDB(t, db, types.EmptyRootHash)
	blk1.SetNonce(addr1, 1)
	root1, err := blk1.Commit(1, true)
	require.NoError(t, err, "blk1.Commit()")
	require.NoErrorf(t, tdb.Commit(root1, false), "triedb.Commit(%s)", root1)

	// References for both divergent states, proposed while root1 is the tip.
	wantX := newStateDB(t, db, root1)
	wantX.SetNonce(addr1, 2)
	wantX.SetNonce(addr2, 1)
	wantRootX := wantX.IntermediateRoot(true)
	wantY := newStateDB(t, db, root1)
	wantY.SetNonce(addr1, 2)
	wantY.SetNonce(addr3, 1)
	wantRootY := wantY.IntermediateRoot(true)

	blk2 := newStateDB(t, db, root1)
	blk2.SetNonce(addr2, 5)
	root2, err := blk2.Commit(2, true)
	require.NoError(t, err, "blk2.Commit()")
	require.NoErrorf(t, tdb.Commit(root2, false), "triedb.Commit(%s)", root2)

	sdb := newStateDB(t, db, root1)
	sdb.SetNonce(addr1, 2)
	sdb.IntermediateRoot(true) // reconstruction is created here

	cp := sdb.Copy()
	sdb.SetNonce(addr2, 1)
	cp.SetNonce(addr3, 1)
	require.Equal(t, wantRootX, sdb.IntermediateRoot(true), "original root after divergence")
	require.Equal(t, wantRootY, cp.IntermediateRoot(true), "copy root after divergence")

	_, err = sdb.Commit(2, true)
	require.ErrorIs(t, err, errHistoricalNotCommittable, "original Commit()")
	_, err = cp.Commit(2, true)
	require.ErrorIs(t, err, errHistoricalNotCommittable, "copy Commit()")
}

// TestHashAfterTipMovesPastParent verifies that a trie whose parent stops being
// proposable still hashes, which needs its revision handle re-taken.
func TestHashAfterTipMovesPastParent(t *testing.T) {
	db := newDB(t)
	tdb := db.TrieDB()

	blk1 := newStateDB(t, db, types.EmptyRootHash)
	blk1.SetNonce(addr1, 1)
	root1, err := blk1.Commit(1, true)
	require.NoError(t, err, "blk1.Commit()")

	// Opened while root1 is still an unpersisted proposal.
	sdb := newStateDB(t, db, root1)
	sdb.SetNonce(addr2, 1)
	require.NotEqual(t, common.Hash{}, sdb.IntermediateRoot(true), "first root")

	// Persist root1 and then move the tip past it, which makes root1
	// unproposable and forces the reconstruction fallback.
	require.NoErrorf(t, tdb.Commit(root1, false), "triedb.Commit(%s)", root1)
	blk2 := newStateDB(t, db, root1)
	blk2.SetNonce(addr3, 1)
	root2, err := blk2.Commit(2, true)
	require.NoError(t, err, "blk2.Commit()")
	require.NoErrorf(t, tdb.Commit(root2, false), "triedb.Commit(%s)", root2)

	sdb.SetNonce(addr2, 2)
	got := sdb.IntermediateRoot(true)
	require.NotEqual(t, common.Hash{}, got, "root after the tip moved past the parent")
	require.NoError(t, sdb.Error(), "StateDB.Error()")

	want := newStateDB(t, db, root1)
	want.SetNonce(addr2, 2)
	require.Equal(t, want.IntermediateRoot(true), got, "root matches a freshly opened trie")
}

// TestHashAfterRevisionEviction verifies that a trie whose parent root has left
// Firewood's in-memory revision window still hashes.
func TestHashAfterRevisionEviction(t *testing.T) {
	cfg := DefaultConfig(t.TempDir(), loggingtest.New(t, logging.Debug))
	cfg.StateHistory = 6
	cfg.DeferredCommitInterval = 5

	// Guarantee one non-empty state root is on disk, since the empty state is always available.
	var root1 common.Hash
	{
		first := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), &triedb.Config{
			DBOverride: cfg.BackendConstructor,
		})
		blk1 := newStateDB(t, first, types.EmptyRootHash)
		blk1.SetNonce(addr1, 1)

		var err error
		root1, err = blk1.Commit(1, true)
		require.NoError(t, err, "blk1.Commit()")
		require.NoErrorf(t, first.TrieDB().Commit(root1, false), "triedb.Commit(%s)", root1)
		require.NoError(t, first.TrieDB().Close(), "triedb.Close()")
	}

	db := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), &triedb.Config{
		DBOverride: cfg.BackendConstructor,
	})

	want := newStateDB(t, db, root1)
	want.SetNonce(addr2, 2)
	wantRoot := want.IntermediateRoot(true)

	// Opened at a persisted root, so its revision handle is historical. Hashing
	// here creates a proposal on the tip.
	sdb := newStateDB(t, db, root1)
	sdb.SetNonce(addr2, 1)
	require.NotEqual(t, common.Hash{}, sdb.IntermediateRoot(true), "first root")

	// Persist RevisionsInMemory newer roots. Firewood drops root1 from its
	// by-hash lookup at that point even though sdb's handle keeps it alive.
	tip := root1
	for i := uint64(2); i <= cfg.StateHistory+1; i++ {
		blk := newStateDB(t, db, tip)
		blk.SetNonce(addr3, i)

		var err error
		tip, err = blk.Commit(i, true)
		require.NoErrorf(t, err, "block %d Commit()", i)
		require.NoErrorf(t, db.TrieDB().Commit(tip, false), "triedb.Commit(%s)", tip)
	}

	_, err := state.New(root1, db, nil)
	wantMissingErr := new(trie.MissingNodeError)
	require.ErrorAs(t, err, &wantMissingErr, "test setup: expected revision to be reaped")

	// After revision is reaped, hashing should still be available
	sdb.SetNonce(addr2, 2)
	require.Equal(t, wantRoot, sdb.IntermediateRoot(true), "root after root1 left the revision window")

	_, err = sdb.Commit(2, true)
	require.ErrorIs(t, err, errHistoricalNotCommittable, "Commit()")
}

func TestStateRootRegressions(t *testing.T) {
	tests := []struct {
		name  string
		steps func(t *testing.T, s *SUT)
	}{
		{
			// testdata/fuzz/FuzzStateRoot/test
			name: "slot_rewritten_to_original_value_across_commits",
			steps: func(t *testing.T, s *SUT) {
				a := common.Address{50}
				s.createAccount(t, a)
				s.updateAccount(t, a)
				s.createAccount(t, common.Address{48})
				s.setStorage(t, a, 48, 48)
				for _, p := range []byte{49, 57, 55, 65} {
					s.createAccount(t, common.Address{p})
				}
				s.stateDBCommit(t)
				s.setStorage(t, a, 48, 49)
				s.createAccount(t, common.Address{56})
				s.stateDBCommit(t)
				s.setStorage(t, a, 48, 48)
				s.stateDBCommit(t)
			},
		},
		{
			name: "resurrect_in_same_tx",
			steps: func(t *testing.T, s *SUT) {
				a := common.Address{}
				s.createAccount(t, a)
				s.setStorage(t, a, 1, 2)
				s.selfDestruct6780(a)
				s.createAccount(t, a)
				s.setStorage(t, a, 3, 4)
			},
		},
		{
			// testdata/fuzz/FuzzStateRoot/4b9a6df5b9d02d03
			name: "copy_between_storage_writes",
			steps: func(t *testing.T, s *SUT) {
				a := common.Address{48}
				s.createAccount(t, a)
				s.finalise()
				s.setStorage(t, a, 55, 48)
				s.copyStateDB()
				s.intermediateRoot(t)
				s.setStorage(t, a, 50, 56)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSUT(
				newBackend(t, state.NewDatabase(rawdb.NewMemoryDatabase())), // hashdb
				newBackend(t, newDB(t)), // firewood
			)
			tt.steps(t, s)
			s.intermediateRoot(t)
		})
	}
}
