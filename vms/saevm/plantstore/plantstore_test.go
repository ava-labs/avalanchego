// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plantstore_test

import (
	"errors"
	"math/big"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/plantstore"

	cchainlibevm "github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm"
)

const testBlocks = 50

// side is one state database the same blocks run on.
type side struct {
	name  string
	sdb   state.Database
	roots []common.Hash // by height
}

func newSide(t *testing.T, name string, c *triedb.Config) *side {
	t.Helper()
	tdb := triedb.NewDatabase(rawdb.NewMemoryDatabase(), c)
	t.Cleanup(func() { require.NoError(t, tdb.Close()) })
	return &side{name: name, sdb: state.NewDatabaseWithNodeDB(rawdb.NewMemoryDatabase(), tdb)}
}

func storeConfig(t *testing.T, scheme string) *triedb.Config {
	t.Helper()
	dir := filepath.Join(t.TempDir(), scheme)
	t.Cleanup(func() { require.NoError(t, plantstore.Release(dir)) })
	switch scheme {
	case plantstore.NullScheme:
		return &triedb.Config{DBOverride: plantstore.NullBackend(dir, nil)}
	case plantstore.OursScheme:
		return &triedb.Config{DBOverride: plantstore.OursBackend(dir, nil)}
	}
	t.Fatalf("scheme %q", scheme)
	return nil
}

func addr(i int) common.Address { return common.BigToAddress(big.NewInt(int64(0x1000 + i))) }

func slotKey(i int) common.Hash { return common.BigToHash(big.NewInt(int64(i))) }

const (
	nAddrs     = 40
	nSlots     = 24
	precompile = 0 // addr(precompile) is a precompile-like account: nonce 1, code 0x01
)

// genesis writes the genesis state: half the accounts, some with code and
// storage, an empty account and the precompile-like account.
func genesis(s *state.StateDB) {
	s.SetNonce(addr(precompile), 1)
	s.SetCode(addr(precompile), []byte{0x01})
	s.CreateAccount(addr(1)) // empty: kept, as genesis commits without EIP-158 deletion
	for i := 2; i < nAddrs/2; i++ {
		a := addr(i)
		s.SetBalance(a, uint256.NewInt(uint64(1000*i)))
		s.SetNonce(a, uint64(i%3))
		if i%3 == 0 {
			s.SetCode(a, []byte{0x60, byte(i), 0x00})
			for j := range nSlots / 2 {
				s.SetState(a, slotKey(j), common.BigToHash(big.NewInt(int64(i*j+1))))
			}
		}
	}
}

// block makes random writes, the same on every side for the same seed: slot
// creates, updates and deletes, balance and nonce changes, new accounts with
// code and storage, self-destructs, emptied accounts and a touch of the
// precompile-like account.
func block(s *state.StateDB, seed uint64) {
	r := rand.New(rand.NewPCG(seed, 7)) //#nosec G404 -- reproducible test data
	for range 30 {
		a := addr(1 + r.IntN(nAddrs-1))
		switch op := r.IntN(10); {
		case op < 4: // slot write: create, update or delete
			v := common.Hash{}
			if r.IntN(4) != 0 {
				v = common.BigToHash(new(big.Int).SetUint64(r.Uint64()))
			}
			if s.GetCodeSize(a) == 0 && s.GetNonce(a) == 0 {
				s.SetNonce(a, 1) // keep a slot-holding account from being emptied
			}
			s.SetState(a, slotKey(r.IntN(nSlots)), v)
		case op < 6:
			s.AddBalance(a, uint256.NewInt(uint64(1+r.IntN(1000))))
		case op < 7:
			s.SetNonce(a, s.GetNonce(a)+1)
		case op < 8: // a contract, possibly where an account was destroyed
			if s.GetCodeSize(a) == 0 {
				s.SetCode(a, []byte{0x60, byte(r.IntN(256)), 0x00})
				s.SetNonce(a, s.GetNonce(a)+1)
			}
		case op < 9: // destroy (legacy SELFDESTRUCT semantics), storage included
			if s.Exist(a) {
				s.SelfDestruct(a)
			}
		default: // empty an account without code or nonce: EIP-158 deletes it
			if s.Exist(a) && s.GetCodeSize(a) == 0 && s.GetNonce(a) == 0 {
				s.SetBalance(a, new(uint256.Int))
			}
		}
	}
	s.AddBalance(addr(precompile), new(uint256.Int)) // touched, not empty: kept
}

// run applies f on the side's newest root and commits it.
func (x *side) run(t *testing.T, f func(*state.StateDB), deleteEmpty bool) common.Hash {
	t.Helper()
	parent := types.EmptyRootHash
	if n := len(x.roots); n > 0 {
		parent = x.roots[n-1]
	}
	s, err := state.New(parent, x.sdb, nil)
	require.NoErrorf(t, err, "%s: state.New(%s)", x.name, parent)
	f(s)
	root, err := s.Commit(uint64(len(x.roots)), deleteEmpty)
	require.NoErrorf(t, err, "%s: Commit at height %d", x.name, len(x.roots))
	x.roots = append(x.roots, root)
	return root
}

// requireSameState checks every account field and slot the test touches at
// the given height of two sides. With wipedOK, a slot read on got may fail
// with [plantstore.ErrWipedHistory] (the account was deleted after height);
// it returns how many accounts did.
func requireSameState(t *testing.T, want, got *side, height int, wipedOK bool) int {
	t.Helper()
	ws, err := state.New(want.roots[height], want.sdb, nil)
	require.NoError(t, err)
	wiped := 0
	for i := range nAddrs {
		// A fresh StateDB per account: a read error sticks to the StateDB.
		gs, err := state.New(got.roots[height], got.sdb, nil)
		require.NoErrorf(t, err, "%s: state.New at height %d", got.name, height)
		a := addr(i)
		require.Equalf(t, ws.Exist(a), gs.Exist(a), "%s height %d: Exist(%d)", got.name, height, i)
		require.Equalf(t, ws.GetBalance(a), gs.GetBalance(a), "%s height %d: GetBalance(%d)", got.name, height, i)
		require.Equalf(t, ws.GetNonce(a), gs.GetNonce(a), "%s height %d: GetNonce(%d)", got.name, height, i)
		require.Equalf(t, ws.GetCodeHash(a), gs.GetCodeHash(a), "%s height %d: GetCodeHash(%d)", got.name, height, i)
		require.Equalf(t, ws.GetCode(a), gs.GetCode(a), "%s height %d: GetCode(%d)", got.name, height, i)
		require.NoErrorf(t, gs.Error(), "%s height %d: account %d", got.name, height, i)
		vals := make([]common.Hash, nSlots)
		for j := range nSlots {
			vals[j] = gs.GetState(a, slotKey(j))
		}
		if err := gs.Error(); wipedOK && errors.Is(err, plantstore.ErrWipedHistory) {
			wiped++
			continue
		}
		require.NoErrorf(t, gs.Error(), "%s height %d: slots of account %d", got.name, height, i)
		for j := range nSlots {
			require.Equalf(t, ws.GetState(a, slotKey(j)), vals[j], "%s height %d: GetState(%d, %d)", got.name, height, i, j)
		}
	}
	require.NoError(t, ws.Error())
	return wiped
}

// drive runs genesis and [testBlocks] random blocks on hashdb and on the
// store, checks every read against hashdb after each block (at the newest
// root and at roots a few blocks back) and, for ours, every root.
func drive(t *testing.T, scheme string) {
	hash := newSide(t, "hashdb", nil)
	st := newSide(t, scheme, storeConfig(t, scheme))

	hg := hash.run(t, genesis, false)
	sg := st.run(t, genesis, false)
	require.Equal(t, hg, sg, "genesis root")
	requireSameState(t, hash, st, 0, false)

	// Only ours keeps no slot history across an account deletion.
	wipedOK := scheme == plantstore.OursScheme
	var wiped int

	for b := 1; b <= testBlocks; b++ {
		f := func(s *state.StateDB) { block(s, uint64(b)) }
		hr := hash.run(t, f, true)
		sr := st.run(t, f, true)
		if scheme == plantstore.OursScheme {
			require.Equalf(t, hr, sr, "block %d root", b)
		}
		requireSameState(t, hash, st, b, false)
		for _, back := range []int{1, 5, 17} {
			if b-back >= 0 {
				wiped += requireSameState(t, hash, st, b-back, wipedOK)
			}
		}
	}
	wiped += requireSameState(t, hash, st, 0, wipedOK) // genesis, still inside the window
	t.Logf("%s: %d older-root account reads met %v", scheme, wiped, plantstore.ErrWipedHistory)
}

func TestNullStore(t *testing.T) { drive(t, plantstore.NullScheme) }

func TestOursStore(t *testing.T) { drive(t, plantstore.OursScheme) }

// TestOursStoreCoreth runs the ours store with the C-Chain's libevm extras:
// five-field (multicoin) account RLP and state-key normalization.
func TestOursStoreCoreth(t *testing.T) {
	require.NoError(t, cchainlibevm.WithTempRegisteredExtras(func() error {
		drive(t, plantstore.OursScheme)
		return nil
	}))
}

// TestStoreWindow checks that a root more than [plantstore.Window] states
// below the newest one is no longer readable, and that the window's oldest is.
func TestStoreWindow(t *testing.T) {
	for _, scheme := range []string{plantstore.NullScheme, plantstore.OursScheme} {
		t.Run(scheme, func(t *testing.T) {
			st := newSide(t, scheme, storeConfig(t, scheme))
			st.run(t, genesis, false)
			for b := 1; b <= plantstore.Window+3; b++ {
				st.run(t, func(s *state.StateDB) {
					s.SetState(addr(3), slotKey(1), common.BigToHash(big.NewInt(int64(b))))
				}, true)
			}
			n := len(st.roots) - 1
			_, err := state.New(st.roots[n-plantstore.Window], st.sdb, nil)
			require.NoError(t, err, "oldest root in the window")
			_, err = state.New(st.roots[n-plantstore.Window-1], st.sdb, nil)
			var missing *trie.MissingNodeError
			require.Truef(t, errors.As(err, &missing), "root below the window: got %v", err)

			s, err := state.New(st.roots[n-plantstore.Window], st.sdb, nil)
			require.NoError(t, err)
			require.Equal(t, common.BigToHash(big.NewInt(int64(n-plantstore.Window))), s.GetState(addr(3), slotKey(1)))
		})
	}
}

// TestStoreRejectsStaleParent checks that a block on a state that is not the
// newest is refused.
func TestStoreRejectsStaleParent(t *testing.T) {
	for _, scheme := range []string{plantstore.NullScheme, plantstore.OursScheme} {
		t.Run(scheme, func(t *testing.T) {
			st := newSide(t, scheme, storeConfig(t, scheme))
			g := st.run(t, genesis, false)
			st.run(t, func(s *state.StateDB) { s.AddBalance(addr(2), uint256.NewInt(1)) }, true)

			s, err := state.New(g, st.sdb, nil)
			require.NoError(t, err)
			s.AddBalance(addr(5), uint256.NewInt(1))
			_, err = s.Commit(1, true)
			require.Error(t, err)
		})
	}
}
