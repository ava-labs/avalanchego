// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateupgrade

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestEqual(t *testing.T) {
	tests := []struct {
		name    string
		upgrade string
		other   string // if empty, round-trips upgrade
		want    bool
	}{
		{
			name:    "round-trip_full_entry",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234","storage":{"0x0000000000000000000000000000000000000000000000000000000000000001":"0x0000000000000000000000000000000000000000000000000000000000000002"},"balanceChange":"100"}}}`,
			want:    true,
		},
		{
			name:    "round-trip_empty_storage_dropped_by_omitempty",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234","storage":{}}}}`,
			want:    true,
		},
		{
			name:    "round-trip_empty_code_dropped_by_omitempty",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x","balanceChange":"0x1"}}}`,
			want:    true,
		},
		{
			name:    "round-trip_decimal_zero_balance_change_re-marshaled_as_hex",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"0"}}}`,
			want:    true,
		},
		{
			name:    "hex_and_decimal_balance_change_compare_by_value",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"100"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"0x64"}}}`,
			want:    true,
		},
		{
			name:    "different_timestamp",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			other:   `{"blockTimestamp":2,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			want:    false,
		},
		{
			name:    "different_code",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x125678"}}}`,
			want:    false,
		},
		{
			name:    "different_storage_value",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"storage":{"0x0000000000000000000000000000000000000000000000000000000000000001":"0x0000000000000000000000000000000000000000000000000000000000000002"}}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"storage":{"0x0000000000000000000000000000000000000000000000000000000000000001":"0x0000000000000000000000000000000000000000000000000000000000000003"}}}}`,
			want:    false,
		},
		{
			name:    "different_balance_change",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"1"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"2"}}}`,
			want:    false,
		},
		{
			name:    "removed_balance_change",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234","balanceChange":"0"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			want:    false,
		},
		{
			name:    "extra_account",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"},"0x0200000000000000000000000000000000000000":{"code":"0xff"}}}`,
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a, b Block
			require.NoError(t, json.Unmarshal([]byte(tt.upgrade), &a), "json.Unmarshal(a)")

			other := []byte(tt.other)
			if len(other) == 0 {
				var err error
				other, err = json.Marshal(a)
				require.NoError(t, err, "json.Marshal(a) for round-trip")
			}
			require.NoError(t, json.Unmarshal(other, &b), "json.Unmarshal(b)")

			require.Equal(t, tt.want, a.Equal(&b), "a.Equal(&b)")
			require.Equal(t, tt.want, b.Equal(&a), "b.Equal(&a)")
		})
	}
}

func TestApply(t *testing.T) {
	var (
		addr     = common.Address{'a', 'd', 'd', 'r'}
		oldCode  = []byte{0x01, 0x02}
		newCode  = []byte{0x03, 0x04, 0x05}
		key1     = common.Hash{1}
		key2     = common.Hash{2}
		oldValue = common.Hash{'o', 'l', 'd'}
		newValue = common.Hash{'n', 'e', 'w'}
	)

	tests := []struct {
		name    string
		eip158  bool
		before  *types.Account // nil if the account doesn't exist
		upgrade Account
		want    types.Account
	}{
		{
			name:   "new_account",
			eip158: true,
			upgrade: Account{
				Code:          newCode,
				Storage:       map[common.Hash]common.Hash{key1: newValue},
				BalanceChange: (*math.HexOrDecimal256)(big.NewInt(50)),
			},
			want: types.Account{
				Balance: big.NewInt(50),
				Nonce:   1,
				Code:    newCode,
				Storage: map[common.Hash]common.Hash{key1: newValue},
			},
		},
		{
			name:    "new_account_pre_eip158",
			upgrade: Account{Code: newCode},
			want: types.Account{
				Balance: big.NewInt(0),
				Nonce:   0,
				Code:    newCode,
			},
		},
		{
			name:   "existing_account",
			eip158: true,
			before: &types.Account{
				Balance: big.NewInt(100),
				Nonce:   5,
				Code:    oldCode,
				Storage: map[common.Hash]common.Hash{key1: oldValue, key2: oldValue},
			},
			upgrade: Account{
				Code:          newCode,
				Storage:       map[common.Hash]common.Hash{key1: newValue},
				BalanceChange: (*math.HexOrDecimal256)(big.NewInt(50)),
			},
			want: types.Account{
				Balance: big.NewInt(150),
				Nonce:   5,
				Code:    newCode,
				Storage: map[common.Hash]common.Hash{key1: newValue, key2: oldValue},
			},
		},
		{
			name:   "no_code_leaves_code_and_nonce",
			eip158: true,
			before: &types.Account{
				Balance: big.NewInt(0),
				Code:    oldCode,
				Storage: map[common.Hash]common.Hash{key1: oldValue},
			},
			upgrade: Account{
				BalanceChange: (*math.HexOrDecimal256)(big.NewInt(50)),
			},
			want: types.Account{
				Balance: big.NewInt(50),
				Code:    oldCode,
				Storage: map[common.Hash]common.Hash{key1: oldValue},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdb, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
			require.NoError(t, err, "state.New()")
			if b := tt.before; b != nil {
				sdb.CreateAccount(addr)
				sdb.SetBalance(addr, uint256.MustFromBig(b.Balance))
				sdb.SetNonce(addr, b.Nonce)
				sdb.SetCode(addr, b.Code)
				for k, v := range b.Storage {
					sdb.SetState(addr, k, v)
				}
			}

			config := &params.ChainConfig{EIP158Block: big.NewInt(1)}
			blockNum := big.NewInt(0)
			if tt.eip158 {
				blockNum = big.NewInt(1)
			}
			require.Equalf(t, tt.eip158, config.IsEIP158(blockNum), "%T.IsEIP158(%d)", config, blockNum)

			upgrade := &Block{
				Timestamp: new(uint64(1)),
				Accounts:  map[common.Address]Account{addr: tt.upgrade},
			}
			upgrade.Apply(config, sdb, blockNum)

			require.True(t, sdb.Exist(addr), "Exist()")
			require.Equal(t, uint256.MustFromBig(tt.want.Balance), sdb.GetBalance(addr), "GetBalance()")
			require.Equal(t, tt.want.Nonce, sdb.GetNonce(addr), "GetNonce()")
			require.Equal(t, tt.want.Code, sdb.GetCode(addr), "GetCode()")
			for _, k := range []common.Hash{key1, key2} {
				require.Equalf(t, tt.want.Storage[k], sdb.GetState(addr, k), "GetState(%v)", k)
			}
		})
	}
}
