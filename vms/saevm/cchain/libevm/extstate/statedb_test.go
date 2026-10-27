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
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm"

	. "github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm/extstate"
)

func TestMain(m *testing.M) {
	libevm.RegisterExtras()
	os.Exit(m.Run())
}

func newEmptyStateDB(tb testing.TB) *StateDB {
	tb.Helper()
	s, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	require.NoError(tb, err, "state.New()")
	return New(s)
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
		wantAssetKey common.Hash
		wantStateKey common.Hash
	}{
		{
			name:         "all_zero",
			wantAssetKey: common.Hash{0x01},
			wantStateKey: common.Hash{0x00},
		},
		{
			name:         "bit_0_set",
			key:          common.Hash{0x0f},
			wantAssetKey: common.Hash{0x0f},
			wantStateKey: common.Hash{0x0e},
		},
		{
			name:         "bit_0_clear",
			key:          common.Hash{0xf0},
			wantAssetKey: common.Hash{0xf1},
			wantStateKey: common.Hash{0xf0},
		},
		{
			name:         "all_ones",
			key:          allOnes,
			wantAssetKey: allOnes,
			wantStateKey: allOnesExceptBit0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newEmptyStateDB(t)
			addr := common.Address{1}
			balance := big.NewInt(1)
			s.AddAssetBalance(addr, tt.key, balance)
			stored := common.Hash{0xff}
			s.SetState(addr, tt.key, stored)

			raw := stateconf.SkipStateKeyTransformation()
			assert.Equalf(t, common.BigToHash(balance), s.GetState(addr, tt.wantAssetKey, raw), "%T.GetState(%s, %s) after %[1]T.AddAssetBalance(%[2]s, %s)", s, addr, tt.wantAssetKey, tt.key)
			assert.Equalf(t, stored, s.GetState(addr, tt.wantStateKey, raw), "%T.GetState(%s, %s) after %[1]T.SetState(%[2]s, %s)", s, addr, tt.wantStateKey, tt.key)
		})
	}
}

// FuzzStateKeyPartition ensures that asset balance updates don't
// affect any existing state with the same key
func FuzzStateKeyPartition(f *testing.F) {
	f.Add([]byte{0x01}) // Already a normalized coin ID.
	f.Fuzz(func(t *testing.T, keyBytes []byte) {
		var key common.Hash
		copy(key[:], keyBytes)

		s := newEmptyStateDB(t)
		addr := common.Address{1}
		want := big.NewInt(10)
		s.AddAssetBalance(addr, key, want)
		stored := common.Hash{0xff}
		s.SetState(addr, key, stored)

		got := s.GetAssetBalance(addr, key)
		require.Zerof(t, want.Cmp(got), "%T.GetAssetBalance(%s, %s) = %d, want %d", s, addr, key, got, want)
		require.Equalf(t, stored, s.GetState(addr, key), "%T.GetState(%s, %s)", s, addr, key)
	})
}

func TestAssetBalance(t *testing.T) {
	type op struct {
		sub    bool
		amount int64
	}
	tests := []struct {
		name                string
		ops                 []op
		want                int64
		wantExist           bool
		wantHeldXChainAsset bool
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
			name:                "add",
			ops:                 []op{{amount: 10}},
			want:                10,
			wantExist:           true,
			wantHeldXChainAsset: true,
		},
		{
			name:                "add_then_sub",
			ops:                 []op{{amount: 10}, {sub: true, amount: 4}},
			want:                6,
			wantExist:           true,
			wantHeldXChainAsset: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newEmptyStateDB(t)
			addr := common.Address{1}
			assetID := common.Hash{2}
			for _, o := range tt.ops {
				if o.sub {
					s.SubAssetBalance(addr, assetID, big.NewInt(o.amount))
				} else {
					s.AddAssetBalance(addr, assetID, big.NewInt(o.amount))
				}
			}

			got := s.GetAssetBalance(addr, assetID)
			assert.Zerof(t, big.NewInt(tt.want).Cmp(got), "%T.GetAssetBalance() = %d, want %d", s, got, tt.want)
			assert.Equalf(t, tt.wantExist, s.Exist(addr), "%T.Exist(%s)", s, addr)
			assert.Equal(t, tt.wantHeldXChainAsset, customtypes.HeldXChainAsset(s.StateDB, addr), "customtypes.HeldXChainAsset()")
		})
	}
}

func TestPrepare(t *testing.T) {
	sender := common.Address{1}
	s := newEmptyStateDB(t)
	rules := cchaintest.ChainConfig().Rules(common.Big0, true /*isMerge*/, 0)
	s.Prepare(rules, sender, common.Address{}, nil, nil, nil)
	assert.Truef(t, s.AddressInAccessList(sender), "%T.AddressInAccessList(%s) after %[1]T.Prepare()", s, sender)
}

func TestGetPredicate(t *testing.T) {
	var (
		predicater = common.Address{1}
		pred       = predicate.Predicate{{2}}
		list       = types.AccessList{{Address: predicater, StorageKeys: pred}}
	)
	rules := cchaintest.ChainConfig().Rules(common.Big0, true /*isMerge*/, 0)
	params.GetRulesExtra(rules).Predicaters[predicater] = nil

	tests := []struct {
		name   string
		lists  []types.AccessList // passed to successive [StateDB.Prepare] calls
		index  int
		want   predicate.Predicate
		wantOK bool
	}{
		{
			name:   "prepared",
			lists:  []types.AccessList{list},
			want:   pred,
			wantOK: true,
		},
		{
			name:  "negative_index",
			lists: []types.AccessList{list},
			index: -1,
		},
		{
			name:  "replaced_by_next_prepare",
			lists: []types.AccessList{list, nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newEmptyStateDB(t)
			for _, l := range tt.lists {
				s.Prepare(rules, common.Address{}, common.Address{}, nil, nil, l)
			}
			got, ok := s.GetPredicate(predicater, tt.index)
			assert.Equalf(t, tt.want, got, "%T.GetPredicate(%s, %d)", s, predicater, tt.index)
			assert.Equalf(t, tt.wantOK, ok, "%T.GetPredicate(%s, %d) ok", s, predicater, tt.index)
		})
	}
}
