// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"encoding/binary"
	"math/big"
	"os"
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
	"github.com/ava-labs/avalanchego/utils/set"
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
	accounts map[common.Address]*accountModel
	addrs    []common.Address // live addresses ordered by creation time

	// Per-transaction tracking, reset at every transaction boundary
	// see [SUT.finaliseTx])
	createdThisTx    set.Set[common.Address]
	destructedThisTx []common.Address // addresses destructed this tx, eligible for resurrection
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

type SUT struct {
	fwDB, hashDB       state.Database
	fwState, hashState *state.StateDB

	lastRoot common.Hash
	blockNum uint64
	counter  uint64 // drives deterministic address/key generation
	model    *fuzzModel
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
			accounts:      make(map[common.Address]*accountModel),
			createdThisTx: set.NewSet[common.Address](0),
		},
	}
}

// nextHash generates a new deterministic hash by hashing the internal counter.
func (s *SUT) nextHash() common.Hash {
	s.counter++
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, s.counter))
}

// createAccount creates an account, modelling an EVM contract deployment. When
// an account has been destructed earlier in the current transaction, that
// accound could be resurrected.
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
	s.model.createdThisTx.Add(addr)

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

// selfDestruct6780 models the SELFDESTRUCT opcode under EIP-6780. The call is
// always issued to the StateDB (it is a legal operation on any account), but it
// only actually destructs the account — and so only updates the model — when the
// account was created in the current transaction. On a pre-existing account it
// is a no-op and the account remains live.
func (s *SUT) selfDestruct6780(param byte) {
	i := int(param) % len(s.model.addrs)
	addr := s.model.addrs[i]

	s.fwState.Selfdestruct6780(addr)
	s.hashState.Selfdestruct6780(addr)

	if !s.model.createdThisTx.Contains(addr) {
		return // not created this tx: Selfdestruct6780 is a no-op
	}

	delete(s.model.accounts, addr)
	s.model.createdThisTx.Remove(addr)
	s.model.destructedThisTx = append(s.model.destructedThisTx, addr)
	s.model.addrs = removeUnordered(s.model.addrs, i)
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

// finaliseTx resets the model's per-transaction tracking to mirror
// [state.StateDB.Finalise].
// Any accounts created this transaction can no longer be selfdestructed,
// and any accounts destructed this transaction can no longer be resurrected.
func (s *SUT) finaliseTx() {
	s.model.createdThisTx.Clear()
	s.model.destructedThisTx = s.model.destructedThisTx[:0]
}

func (s *SUT) finalise() {
	s.fwState.Finalise(true /* EIP-158 */)
	s.hashState.Finalise(true /* EIP-158 */)
	s.finaliseTx()
}

func (s *SUT) intermediateRoot(t *testing.T) {
	fwRoot := s.fwState.IntermediateRoot(true /* EIP-158 */)
	hashRoot := s.hashState.IntermediateRoot(true /* EIP-158 */)
	require.Equal(t, hashRoot, fwRoot, "root mismatch")
	s.finaliseTx() // IntermediateRoot calls Finalise
}

func (s *SUT) stateDBCommit(t *testing.T) {
	hashRoot, err := s.hashState.Commit(s.blockNum, true /* EIP-158 */)
	require.NoError(t, err)
	fwRoot, err := s.fwState.Commit(s.blockNum, true /* EIP-158 */)
	require.NoError(t, err)
	require.Equal(t, hashRoot, fwRoot, "root mismatch after commit")

	s.lastRoot = fwRoot
	s.blockNum++

	s.fwState = mustNewStateDB(t, s.fwDB, s.lastRoot)
	s.hashState = mustNewStateDB(t, s.hashDB, s.lastRoot)
	s.finaliseTx() // Commit calls Finalise
}

func (s *SUT) diskCommit(t *testing.T) {
	require.NoErrorf(t, s.fwDB.TrieDB().Commit(s.lastRoot, false), "triedb.Commit(%s)", s.lastRoot)
	require.NoErrorf(t, s.hashDB.TrieDB().Commit(s.lastRoot, false), "triedb.Commit(%s)", s.lastRoot)
}

func (s *SUT) copyStateDB() {
	s.fwState = s.fwState.Copy()
	s.hashState = s.hashState.Copy()
}

// TODO(#5539): support [*state.StateDB.SelfDestruct]
const (
	opStateDBCommit    byte = iota // commit pending changes; root changes iff state changed
	opDiskCommit                   // flush pending, then commit a clean StateDB; root must be unchanged
	opCreateAccount                // deploy an account; resurrects a same-tx destructed account when one exists
	opUpdateAccount                // increment nonce and balance of an existing account
	opSelfDestruct6780             // SELFDESTRUCT (EIP-6780) an account; no-op unless created this tx
	opFinalise                     // end the current transaction without computing a root
	opSetStorage                   // write a new storage slot on an existing account
	opDeleteStorage                // zero out an existing storage slot on an existing account
	opIntermediateRoot             // verify all account and storage hashes match the model
	opCopyStateDB                  // copies statedb for future ops
	maxOp
)

// FuzzStateDB compares the state root of an arbitrary sequence of operations on
// a Firewood-backed [state.StateDB] against a reference HashDB.
func FuzzStateDB(f *testing.F) {
	f.Add([]byte{
		opCreateAccount,
		opStateDBCommit,
	})
	f.Add([]byte{
		opCreateAccount,
		opUpdateAccount,
		opSetStorage,
		opStateDBCommit,
		opDiskCommit,
	})
	f.Add([]byte{
		opCreateAccount,
		opSetStorage,
		opIntermediateRoot,
		opDeleteStorage,
		opStateDBCommit,
	})
	f.Add([]byte{opStateDBCommit, opDiskCommit})
	f.Add([]byte{
		opCreateAccount,
		opFinalise,
		opSelfDestruct6780,
		opStateDBCommit,
	})
	f.Add([]byte{
		opCreateAccount,
		opSetStorage,
		opSelfDestruct6780,
		opCreateAccount, // resurrects the just-destructed account
		opSetStorage,
		opStateDBCommit,
	})

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
			case opSelfDestruct6780:
				if len(sut.model.addrs) > 0 {
					t.Log("delete account")
					sut.selfDestruct6780(param)
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
			case opFinalise:
				t.Log("finalise (end transaction)")
				sut.finalise()
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
		sdb.SetBalance(addr, uint256.NewInt(uint64(i))) //#nosec G115 // different balance to force different root
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
		root, err := sdb.Commit(uint64(i), true) //#nosec G115 // guaranteed to be positive
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
