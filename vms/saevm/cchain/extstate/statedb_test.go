// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate_test

import (
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/extstate"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm/customtypes"
)

func TestMain(m *testing.M) {
	libevm.RegisterExtras()
	os.Exit(m.Run())
}

func newEmptyStateDB(tb testing.TB) *extstate.StateDB {
	tb.Helper()
	s, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	require.NoError(tb, err, "state.New()")
	return extstate.New(s)
}

func TestStateKeyPartition(t *testing.T) {
	allOnes := common.Hash{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	}
	allOnesExceptBit0 := allOnes
	allOnesExceptBit0[0] = 0xfe

	tests := []struct {
		name         string
		key          common.Hash
		wantCoinID   common.Hash
		wantStateKey common.Hash
	}{
		{
			name:         "all_zero",
			wantCoinID:   common.Hash{0x01},
			wantStateKey: common.Hash{0x00},
		},
		{
			name:         "bit_0_set",
			key:          common.Hash{0x0f},
			wantCoinID:   common.Hash{0x0f},
			wantStateKey: common.Hash{0x0e},
		},
		{
			name:         "bit_0_clear",
			key:          common.Hash{0xf0},
			wantCoinID:   common.Hash{0xf1},
			wantStateKey: common.Hash{0xf0},
		},
		{
			name:         "all_ones",
			key:          allOnes,
			wantCoinID:   allOnes,
			wantStateKey: allOnesExceptBit0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newEmptyStateDB(t)
			addr := common.Address{1}
			balance := big.NewInt(1)
			s.AddBalanceMultiCoin(addr, tt.key, balance)
			stored := common.Hash{0xff}
			s.SetState(addr, tt.key, stored)

			raw := stateconf.SkipStateKeyTransformation()
			assert.Equalf(t, common.BigToHash(balance), s.GetState(addr, tt.wantCoinID, raw), "%T.GetState(%s, %s) after %[1]T.AddBalanceMultiCoin(%[2]s, %s)", s, addr, tt.wantCoinID, tt.key)
			assert.Equalf(t, stored, s.GetState(addr, tt.wantStateKey, raw), "%T.GetState(%s, %s) after %[1]T.SetState(%[2]s, %s)", s, addr, tt.wantStateKey, tt.key)
		})
	}
}

// FuzzStateKeyPartition covers [extstate.StateDBHooks]. Without them, a storage
// write to a key equal to a coin ID would overwrite that coin's balance.
func FuzzStateKeyPartition(f *testing.F) {
	f.Add([]byte{0x01}) // Already a normalized coin ID.
	f.Fuzz(func(t *testing.T, keyBytes []byte) {
		var key common.Hash
		copy(key[:], keyBytes)

		s := newEmptyStateDB(t)
		addr := common.Address{1}
		want := big.NewInt(10)
		s.AddBalanceMultiCoin(addr, key, want)
		stored := common.Hash{0xff}
		s.SetState(addr, key, stored)

		got := s.GetBalanceMultiCoin(addr, key)
		require.Zerof(t, want.Cmp(got), "%T.GetBalanceMultiCoin(%s, %s) = %d, want %d", s, addr, key, got, want)
		require.Equalf(t, stored, s.GetState(addr, key), "%T.GetState(%s, %s)", s, addr, key)
	})
}

func TestMultiCoinBalance(t *testing.T) {
	type op struct {
		sub    bool
		amount int64
	}
	tests := []struct {
		name          string
		ops           []op
		want          int64
		wantExist     bool
		wantMultiCoin bool
	}{
		{
			name:      "add_zero",
			ops:       []op{{}},
			wantExist: true,
		},
		{
			name: "sub_zero",
			ops:  []op{{sub: true}},
		},
		{
			name:          "add",
			ops:           []op{{amount: 10}},
			want:          10,
			wantExist:     true,
			wantMultiCoin: true,
		},
		{
			name:          "add_then_sub",
			ops:           []op{{amount: 10}, {sub: true, amount: 4}},
			want:          6,
			wantExist:     true,
			wantMultiCoin: true,
		},
		{
			name:          "sub_entire_balance",
			ops:           []op{{amount: 10}, {sub: true, amount: 10}},
			wantExist:     true,
			wantMultiCoin: true,
		},
		{
			name:          "add_sub_add",
			ops:           []op{{amount: 10}, {sub: true, amount: 5}, {amount: 3}},
			want:          8,
			wantExist:     true,
			wantMultiCoin: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newEmptyStateDB(t)
			addr := common.Address{1}
			coinID := common.Hash{2}
			for _, o := range tt.ops {
				if o.sub {
					s.SubBalanceMultiCoin(addr, coinID, big.NewInt(o.amount))
				} else {
					s.AddBalanceMultiCoin(addr, coinID, big.NewInt(o.amount))
				}
			}

			got := s.GetBalanceMultiCoin(addr, coinID)
			assert.Zerof(t, big.NewInt(tt.want).Cmp(got), "%T.GetBalanceMultiCoin() = %d, want %d", s, got, tt.want)
			assert.Equalf(t, tt.wantExist, s.Exist(addr), "%T.Exist(%s)", s, addr)
			assert.Equal(t, tt.wantMultiCoin, customtypes.IsMultiCoin(s.StateDB, addr), "customtypes.IsMultiCoin()")
		})
	}
}

func TestMultiCoinSnapshotGeneration(t *testing.T) {
	diskdb := rawdb.NewMemoryDatabase()
	db := state.NewDatabase(diskdb)

	addr := common.BytesToAddress([]byte("addr1"))
	coinID := common.Hash{0x02}
	want := big.NewInt(10)

	statedb, err := state.New(types.EmptyRootHash, db, nil)
	require.NoError(t, err, "state.New()")
	s := extstate.New(statedb)
	s.AddBalanceMultiCoin(addr, coinID, want)
	root, err := s.Commit(0, false)
	require.NoErrorf(t, err, "%T.Commit()", s)
	triedb := db.TrieDB()
	require.NoErrorf(t, triedb.Commit(root, true), "%T.Commit()", triedb)

	snaps, err := snapshot.New(
		snapshot.Config{
			CacheSize:  16,
			SkipVerify: true,
		},
		diskdb,
		triedb,
		common.Hash{},
		root,
	)
	require.NoError(t, err, "snapshot.New()")
	snap := snaps.Snapshot(root)

	addrHash := crypto.Keccak256Hash(addr[:])
	account, err := snap.Account(addrHash)
	require.NoErrorf(t, err, "%T.Account()", snap)
	require.True(t, customtypes.IsAccountMultiCoin(account), "customtypes.IsAccountMultiCoin()")

	slot := common.Hash{0x03} // coinID with bit 0 set.
	storage, err := snap.Storage(addrHash, crypto.Keccak256Hash(slot[:]))
	require.NoErrorf(t, err, "%T.Storage()", snap)
	got := new(big.Int).SetBytes(storage)
	require.Zerof(t, want.Cmp(got), "%T.Storage() = %d, want %d", snap, got, want)
}

func TestPrepare(t *testing.T) {
	var (
		addrA        = common.Address{1}
		addrB        = common.Address{2}
		noPredicater = common.Address{3}
		sender       = common.Address{4}
		transientKey = common.Hash{5}
		keysA0       = []common.Hash{{0xa0}}
		keysA1       = []common.Hash{{0xa1}}
		keysB0       = []common.Hash{{0xb0}}
	)
	rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
	predicaters := params.GetRulesExtra(rules).Predicaters
	predicaters[addrA] = nil
	predicaters[addrB] = nil

	s := newEmptyStateDB(t)
	s.SetTransientState(addrA, transientKey, common.Hash{1})
	s.Prepare(
		rules,
		sender,
		common.Address{},
		nil,
		nil,
		types.AccessList{
			{Address: addrA, StorageKeys: keysA0},
			{Address: noPredicater, StorageKeys: []common.Hash{{0xc0}}},
			{Address: addrB, StorageKeys: keysB0},
			{Address: addrA, StorageKeys: keysA1},
		},
	)
	assert.Truef(t, s.AddressInAccessList(sender), "%T.AddressInAccessList(%s)", s, sender)
	assert.Zerof(t, s.GetTransientState(addrA, transientKey), "%T.GetTransientState(%s, %s)", s, addrA, transientKey)

	tests := []struct {
		name   string
		addr   common.Address
		index  int
		want   predicate.Predicate
		wantOK bool
	}{
		{
			name:   "first_for_address",
			addr:   addrA,
			want:   keysA0,
			wantOK: true,
		},
		{
			name:   "second_for_address",
			addr:   addrA,
			index:  1,
			want:   keysA1,
			wantOK: true,
		},
		{
			name:   "other_address",
			addr:   addrB,
			want:   keysB0,
			wantOK: true,
		},
		{
			name:  "index_past_end",
			addr:  addrA,
			index: 2,
		},
		{
			name:  "negative_index",
			addr:  addrA,
			index: -1,
		},
		{
			name: "address_without_predicater",
			addr: noPredicater,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := s.GetPredicate(tt.addr, tt.index)
			assert.Equalf(t, tt.want, got, "%T.GetPredicate(%s, %d)", s, tt.addr, tt.index)
			assert.Equalf(t, tt.wantOK, ok, "%T.GetPredicate(%s, %d) ok", s, tt.addr, tt.index)
		})
	}

	// A new transaction MUST NOT see predicates from the previous transaction.
	s.Prepare(
		rules,
		common.Address{},
		common.Address{},
		nil,
		nil,
		nil,
	)
	got, ok := s.GetPredicate(addrA, 0)
	assert.Nilf(t, got, "%T.GetPredicate(%s, 0)", s, addrA)
	assert.Falsef(t, ok, "%T.GetPredicate(%s, 0) ok", s, addrA)
}
