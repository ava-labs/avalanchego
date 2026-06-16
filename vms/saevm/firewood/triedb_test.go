// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"encoding/binary"
	"math/big"
	"os"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
)

func mustNewDB(t *testing.T) state.Database {
	t.Helper()

	cfg := DefaultConfig(t.TempDir(), loggingtest.New(t, logging.Debug))
	db := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), &triedb.Config{
		DBOverride: cfg.BackendConstructor,
	})
	t.Cleanup(func() {
		require.NoError(t, db.TrieDB().Close(), "triedb.Close()")
	})
	return db
}

func mustNewStateDB(t *testing.T, db state.Database, root common.Hash) *state.StateDB {
	t.Helper()

	st, err := state.New(root, db, nil)
	require.NoErrorf(t, err, "state.New(%s, %T)", root, db)

	return st
}

// accountModel is the in-memory reference for a single account.
type accountModel struct {
	nonce   uint64
	balance *uint256.Int
	storage []common.Hash // list of active keys
}

type fuzzModel struct {
	accounts   map[common.Address]*accountModel
	addrs      []common.Address // live addresses ordered by creation time
	destructed []common.Address // self-destructed addresses available for recreation
}

func (m *fuzzModel) selectAddr(param byte) common.Address {
	return m.addrs[int(param)%len(m.addrs)]
}

// removeUnordered removes the element at index i by swapping in the last
// element and truncating. Order is not preserved.
func removeUnordered[T any](s []T, i int) []T {
	last := len(s) - 1
	s[i] = s[last]
	return s[:last]
}

// clone returns a deep copy of the model, used to restore it on revert.
func (m *fuzzModel) clone() *fuzzModel {
	accounts := make(map[common.Address]*accountModel, len(m.accounts))
	for addr, acc := range m.accounts {
		accounts[addr] = &accountModel{
			nonce:   acc.nonce,
			balance: acc.balance.Clone(),
			storage: slices.Clone(acc.storage),
		}
	}
	return &fuzzModel{
		accounts:   accounts,
		addrs:      slices.Clone(m.addrs),
		destructed: slices.Clone(m.destructed),
	}
}

// snapshotRef pairs the snapshot IDs of both databases with the model state
// at the time the snapshot was taken.
type snapshotRef struct {
	fwID, hashID int
	model        *fuzzModel
}

type SUT struct {
	fwDB, hashDB       state.Database
	fwState, hashState *state.StateDB

	lastRoot  common.Hash
	blockNum  uint64
	counter   uint64 // drives deterministic address/key generation
	model     *fuzzModel
	snapshots []snapshotRef
}

func newSUT(t *testing.T) *SUT {
	fwDB := mustNewDB(t)
	hashDB := state.NewDatabase(rawdb.NewMemoryDatabase())

	root := types.EmptyRootHash
	fwState := mustNewStateDB(t, fwDB, root)
	hashState := mustNewStateDB(t, hashDB, root)
	return &SUT{
		fwDB:      fwDB,
		fwState:   fwState,
		hashDB:    hashDB,
		hashState: hashState,
		lastRoot:  root,
		model: &fuzzModel{
			accounts: make(map[common.Address]*accountModel),
		},
	}
}

// nextHash generates a new deterministic hash by hashing the internal counter.
func (s *SUT) nextHash() common.Hash {
	s.counter++
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, s.counter))
}

// createAccount models an EVM contract deployment. When a previously
// self-destructed account is available, an even param resurrects it at the
// same address instead of allocating a new one. Same-block resurrection
// exercises the path where [state.StateDB] clears the prior incarnation's
// storage via [state.StateDB.Commit] rather than calling
// [state.Trie.DeleteAccount].
func (s *SUT) createAccount(param byte) {
	var addr common.Address
	if len(s.model.destructedThisTx) > 0 && param&1 == 0 {
		// choose a previously destructed account to resurrect.
		i := int(param>>1) % len(s.model.destructedThisTx)
		addr = s.model.destructedThisTx[i]
		s.model.destructedThisTx = removeUnordered(s.model.destructedThisTx, i)
	} else {
		addr = common.BytesToAddress(s.nextHash().Bytes())
	}

	bal := uint256.NewInt(100)
	s.model.accounts[addr] = &accountModel{
		nonce:   1,
		balance: bal.Clone(),
	}
	s.model.addrs = append(s.model.addrs, addr)

	s.fwState.CreateAccount(addr)
	s.fwState.SetNonce(addr, 1)
	s.fwState.SetBalance(addr, bal)
	s.hashState.CreateAccount(addr)
	s.hashState.SetNonce(addr, 1)
	s.hashState.SetBalance(addr, bal)
}

func (s *SUT) updateAccount(param byte) {
	addr := s.model.selectAddr(param)
	acc := s.model.accounts[addr]
	acc.nonce++
	acc.balance = new(uint256.Int).Add(acc.balance, uint256.NewInt(1))

	s.fwState.SetNonce(addr, acc.nonce)
	s.fwState.SetBalance(addr, acc.balance)
	s.hashState.SetNonce(addr, acc.nonce)
	s.hashState.SetBalance(addr, acc.balance)
}

func (s *SUT) deleteAccount(param byte) {
	i := int(param) % len(s.model.addrs)
	addr := s.model.addrs[i]
	delete(s.model.accounts, addr)
	s.model.addrs = removeUnordered(s.model.addrs, i)
	s.model.destructed = append(s.model.destructed, addr)

	s.fwState.SelfDestruct(addr)
	s.hashState.SelfDestruct(addr)
}

func (s *SUT) setStorage(param byte) {
	addr := s.model.selectAddr(param)
	key := s.nextHash()
	val := s.nextHash()
	s.model.accounts[addr].storage = append(s.model.accounts[addr].storage, key)

	s.fwState.SetState(addr, key, val)
	s.hashState.SetState(addr, key, val)
}

func (s *SUT) deleteStorage(param byte) {
	addr := s.model.selectAddr(param)
	storage := s.model.accounts[addr].storage
	if len(storage) == 0 {
		return
	}
	i := int(param) % len(storage)
	key := storage[i]
	s.model.accounts[addr].storage = removeUnordered(storage, i)

	s.fwState.SetState(addr, key, common.Hash{})
	s.hashState.SetState(addr, key, common.Hash{})
}

// getStorage reads a committed storage slot — either one that was previously
// written, or a never-written one. The returned values are deliberately NOT
// compared: trie-level read semantics are limited (see "Why reads aren't
// safe" in the README). The read itself MUST NOT mutate state; the root
// comparisons in later operations verify this.
func (s *SUT) getStorage(param byte) {
	addr := s.model.selectAddr(param)
	key := s.nextHash() // a slot that was never written
	if storage := s.model.accounts[addr].storage; len(storage) > 0 && param%2 == 0 {
		key = storage[int(param)%len(storage)]
	}

	s.fwState.GetCommittedState(addr, key)
	s.hashState.GetCommittedState(addr, key)
}

// snapshot records a [state.StateDB.Snapshot] on both databases together with
// a copy of the model, so a later revert restores all three in lockstep.
func (s *SUT) snapshot() {
	s.snapshots = append(s.snapshots, snapshotRef{
		fwID:   s.fwState.Snapshot(),
		hashID: s.hashState.Snapshot(),
		model:  s.model.clone(),
	})
}

// revertToSnapshot reverts both databases and the model to a previously
// recorded snapshot. The chosen snapshot and all later ones are discarded,
// matching [state.StateDB.RevertToSnapshot], which invalidates them.
func (s *SUT) revertToSnapshot(param byte) {
	i := int(param) % len(s.snapshots)
	ref := s.snapshots[i]
	s.fwState.RevertToSnapshot(ref.fwID)
	s.hashState.RevertToSnapshot(ref.hashID)
	s.model = ref.model
	s.snapshots = s.snapshots[:i]
}

// dropSnapshots discards all recorded snapshots. Snapshots MUST NOT be
// reverted across a Finalise or a Copy, mirroring the EVM, which only reverts
// within a single transaction.
func (s *SUT) dropSnapshots() {
	s.snapshots = nil
}

func (s *SUT) intermediateRoot(t *testing.T) {
	s.dropSnapshots()
	fwRoot := s.fwState.IntermediateRoot(true /* EIP-158 */)
	hashRoot := s.hashState.IntermediateRoot(true /* EIP-158 */)
	require.Equal(t, hashRoot, fwRoot, "root mismatch")
}

func (s *SUT) stateDBCommit(t *testing.T) {
	s.dropSnapshots()
	hashRoot, err := s.hashState.Commit(s.blockNum, true /* EIP-158 */)
	require.NoError(t, err)
	fwRoot, err := s.fwState.Commit(s.blockNum, true /* EIP-158 */)
	require.NoError(t, err)
	require.Equal(t, hashRoot, fwRoot, "root mismatch after commit")

	s.lastRoot = fwRoot
	s.blockNum++

	s.fwState = mustNewStateDB(t, s.fwDB, s.lastRoot)
	s.hashState = mustNewStateDB(t, s.hashDB, s.lastRoot)
}

func (s *SUT) diskCommit(t *testing.T) {
	require.NoErrorf(t, s.fwDB.TrieDB().Commit(s.lastRoot, false), "triedb.Commit(%s)", s.lastRoot)
	require.NoErrorf(t, s.hashDB.TrieDB().Commit(s.lastRoot, false), "triedb.Commit(%s)", s.lastRoot)
}

func (s *SUT) copyStateDB() {
	s.dropSnapshots()
	s.fwState = s.fwState.Copy()
	s.hashState = s.hashState.Copy()
}

const (
	// Account ops.
	opCreateAccount byte = iota // add a new account, or resurrect a self-destructed one
	opUpdateAccount             // increment nonce and balance of an existing account
	opDeleteAccount             // delete an existing account via [state.StateDB.SelfDestruct]
	// Storage ops.
	opSetStorage    // write a new storage slot on an existing account
	opDeleteStorage // zero out an existing storage slot on an existing account
	opGetStorage    // read a committed storage slot; must not mutate state
	// Snapshot ops.
	opSnapshot         // record a snapshot of both databases and the model
	opRevertToSnapshot // revert both databases and the model to a recorded snapshot
	// Lifecycle ops.
	opIntermediateRoot // verify all account and storage hashes match the model
	opStateDBCommit    // commit pending changes; root changes iff state changed
	opDiskCommit       // flush pending, then commit a clean StateDB; root must be unchanged
	opCopyStateDB      // copies statedb for future ops
	maxOp
)

// FuzzStateDB compares the state root of an arbitrary sequence of operations on
// a Firewood-backed [state.StateDB] against a reference HashDB.
func FuzzStateDB(f *testing.F) {
	seeds := []struct {
		name string
		ops  []byte
	}{
		{name: "create then commit", ops: []byte{opCreateAccount, opStateDBCommit}},
		{
			name: "update, store, then flush to disk",
			ops:  []byte{opCreateAccount, opUpdateAccount, opSetStorage, opStateDBCommit, opDiskCommit},
		},
		{
			name: "store, hash, delete storage, commit",
			ops:  []byte{opCreateAccount, opSetStorage, opIntermediateRoot, opDeleteStorage, opStateDBCommit},
		},
		{
			name: "create, commit, self-destruct, commit",
			ops:  []byte{opCreateAccount, opStateDBCommit, opDeleteAccount, opStateDBCommit},
		},
		{name: "empty commits", ops: []byte{opStateDBCommit, opDiskCommit}},
		{
			// The destructed account is prefix-deleted at the intervening
			// commit, so the recreated account starts with empty storage.
			name: "recreate in a later block than the self-destruct",
			ops: []byte{
				opCreateAccount, opSetStorage, opStateDBCommit,
				opDeleteAccount, opStateDBCommit,
				opCreateAccount, opSetStorage, opStateDBCommit,
			},
		},
		{
			// The account's prior storage must not leak into the recreated
			// account's state.
			name: "recreate in the same block as the self-destruct",
			ops: []byte{
				opCreateAccount, opSetStorage, opStateDBCommit,
				opDeleteAccount, opCreateAccount, opStateDBCommit,
			},
		},
		{
			// The prior incarnation's storage was never committed: it exists
			// only in the trie's pending update operations, and the copy resets
			// the trie's reader to the pre-block state.
			name: "same-block recreate with uncommitted prior storage, after a copy",
			ops: []byte{
				opCreateAccount, opSetStorage, opIntermediateRoot,
				opCopyStateDB,
				opDeleteAccount, opCreateAccount,
			},
		},
		{
			// Reading a never-written slot opens a storage trie with no
			// subsequent account write; the read must not affect the root.
			name: "read a never-written slot",
			ops: []byte{
				opCreateAccount, opStateDBCommit,
				opGetStorage, opStateDBCommit,
			},
		},
		{
			// The committed root must equal the pre-block root.
			name: "fully reverted self-destruct and recreation",
			ops: []byte{
				opCreateAccount, opSetStorage, opStateDBCommit,
				opSnapshot,
				opDeleteAccount, opCreateAccount,
				opRevertToSnapshot, opStateDBCommit,
			},
		},
		{
			name: "reverted self-destruct keeps the account and storage",
			ops: []byte{
				opCreateAccount, opSetStorage,
				opSnapshot,
				opDeleteAccount,
				opRevertToSnapshot, opStateDBCommit,
			},
		},
		{
			name: "reverted self-destruct, recreation, and new storage",
			ops: []byte{
				opCreateAccount, opSetStorage, opStateDBCommit,
				opSnapshot,
				opDeleteAccount, opCreateAccount, opSetStorage,
				opRevertToSnapshot, opStateDBCommit,
			},
		},
	}
	for _, seed := range seeds {
		f.Add(seed.ops)
	}

	f.Fuzz(func(t *testing.T, steps []byte) {
		sut := newSUT(t)
		for _, step := range steps {
			op := step % maxOp
			param := step / maxOp
			switch op {
			case opCreateAccount:
				t.Log("create account")
				sut.createAccount(param)
			case opUpdateAccount:
				if len(sut.model.addrs) > 0 {
					t.Log("update account")
					sut.updateAccount(param)
				}
			case opDeleteAccount:
				if len(sut.model.addrs) > 0 {
					t.Log("delete account")
					sut.deleteAccount(param)
				}
			case opSetStorage:
				if len(sut.model.addrs) > 0 {
					t.Log("set storage")
					sut.setStorage(param)
				}
			case opDeleteStorage:
				if len(sut.model.addrs) > 0 {
					t.Log("delete storage")
					sut.deleteStorage(param)
				}
			case opGetStorage:
				if len(sut.model.addrs) > 0 {
					t.Log("get storage")
					sut.getStorage(param)
				}
			case opSnapshot:
				t.Log("snapshot")
				sut.snapshot()
			case opRevertToSnapshot:
				if len(sut.snapshots) > 0 {
					t.Log("revert to snapshot")
					sut.revertToSnapshot(param)
				}
			case opIntermediateRoot:
				t.Log("intermediate root (end transaction)")
				sut.intermediateRoot(t)
			case opStateDBCommit:
				t.Log("commit (end block)")
				sut.stateDBCommit(t)
			case opDiskCommit:
				t.Log("disk commit (previous block)")
				sut.diskCommit(t)
			case opCopyStateDB:
				t.Log("copy StateDB")
				sut.copyStateDB()
			default:
				t.Skip("invalid operation")
			}
		}
		sut.intermediateRoot(t) // final flush to tries and verify all pending state
	})
}

func TestGenesis(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig(dir, loggingtest.New(t, logging.Debug))
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
	require.NoError(t, err)
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

// TestMultipleTries verifies that the TrieDB is not informed any changes
// unless [state.StateDB.Commit] is called.
// TODO(#5506): This should be safe concurrently as well.
func TestMultipleTries(t *testing.T) {
	db := mustNewDB(t)

	sdb1 := mustNewStateDB(t, db, types.EmptyRootHash)
	sdb2 := mustNewStateDB(t, db, types.EmptyRootHash)
	addr := common.BytesToAddress([]byte{1})
	for i, sdb := range []*state.StateDB{sdb1, sdb2} {
		sdb.CreateAccount(addr)
		sdb.SetNonce(addr, 1)
		sdb.SetBalance(addr, uint256.NewInt(uint64(i))) // different balance to force different root
		_ = sdb.IntermediateRoot(true)                  // force proposal creation
	}

	// Only one can be committed, choose first
	root1, err := sdb1.Commit(0, true)
	require.NoError(t, err, "sdb1.Commit()")
	require.NoErrorf(t, db.TrieDB().Commit(root1, false), "triedb.Commit(%s)", root1)
}

// TestMultipleProposals verifies that a single [triedb.Commit] call
// chains the commit of dependent proposals.
func TestMultipleProposals(t *testing.T) {
	db := mustNewDB(t)

	const numBlocks = 5
	lastRoot := types.EmptyRootHash
	for i := range numBlocks {
		addr := common.BytesToAddress([]byte{byte(i)})
		sdb := mustNewStateDB(t, db, lastRoot)
		sdb.CreateAccount(addr)
		sdb.SetNonce(addr, 1)
		sdb.SetBalance(addr, uint256.NewInt(0))
		root, err := sdb.Commit(uint64(i), true)
		require.NoError(t, err, "sdb.Commit()")
		lastRoot = root
	}

	require.NoErrorf(t, db.TrieDB().Commit(lastRoot, false), "triedb.Commit(%s)", lastRoot)
}

func TestInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func(TrieDBConfig) TrieDBConfig
		wantErr error
	}{
		{
			name: "empty directory",
			cfg: func(cfg TrieDBConfig) TrieDBConfig {
				cfg.DatabaseDir = ""
				return cfg
			},
			wantErr: errDatabaseDirNotProvided,
		},
		{
			name: "file instead of directory",
			cfg: func(cfg TrieDBConfig) TrieDBConfig {
				file := t.TempDir() + "/file"
				require.NoError(t, os.WriteFile(file, []byte("not a directory"), 0o600))
				cfg.DatabaseDir = file
				return cfg
			},
			wantErr: errNotDirectory,
		},
		{
			name: "too few revisions",
			cfg: func(cfg TrieDBConfig) TrieDBConfig {
				cfg.RevisionsInMemory = 1
				return cfg
			},
			wantErr: errTooFewRevisions,
		},
		{
			name: "commit interval too big",
			cfg: func(cfg TrieDBConfig) TrieDBConfig {
				cfg.DeferredCommitInterval = 5
				cfg.RevisionsInMemory = 5
				return cfg
			},
			wantErr: errCommitIntervalTooBig,
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

func TestNoLoggerPanics(t *testing.T) {
	cfg := DefaultConfig(t.TempDir(), nil)
	require.Panics(t, func() {
		_, _ = New(cfg)
	}, "New()")
}
