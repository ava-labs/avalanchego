// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/libevm/ethtest"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOp_ApplyTo(t *testing.T) {
	var (
		eoa         = common.Address{0x00}
		eoaMaxNonce = common.Address{0x01}
	)
	_, _, db := ethtest.NewEmptyStateDB(t)
	db.SetNonce(eoaMaxNonce, math.MaxUint64)

	type account struct {
		address common.Address
		nonce   uint64
		balance *uint256.Int
	}
	steps := []struct {
		name         string
		op           *Op
		wantAccounts []account
		wantErr      error
	}{
		{
			name: "mint_to_eoa",
			op: &Op{
				Mint: map[common.Address]uint256.Int{
					eoa: *uint256.NewInt(1_000_000),
				},
			},
			wantAccounts: []account{
				{
					address: eoa,
					nonce:   0,
					balance: uint256.NewInt(1_000_000),
				},
				{
					address: eoaMaxNonce,
					nonce:   math.MaxUint64,
					balance: uint256.NewInt(0),
				},
			},
		},
		{
			name: "transfer_from_eoa_to_eoaMaxNonce",
			op: &Op{
				Burn: map[common.Address]AccountDebit{
					eoa: {
						Amount:     *uint256.NewInt(100_000),
						MinBalance: *uint256.NewInt(100_000),
					},
				},
				Mint: map[common.Address]uint256.Int{
					eoaMaxNonce: *uint256.NewInt(100_000),
				},
			},
			wantAccounts: []account{
				{
					address: eoa,
					nonce:   1,
					balance: uint256.NewInt(900_000),
				},
				{
					address: eoaMaxNonce,
					nonce:   math.MaxUint64,
					balance: uint256.NewInt(100_000),
				},
			},
		},
		{
			name: "burn_all_funds",
			op: &Op{
				Burn: map[common.Address]AccountDebit{
					eoa: {
						Amount:     *uint256.NewInt(900_000),
						MinBalance: *uint256.NewInt(900_000),
					},
					eoaMaxNonce: {
						Amount:     *uint256.NewInt(100_000),
						MinBalance: *uint256.NewInt(100_000),
					},
				},
			},
			wantAccounts: []account{
				{
					address: eoa,
					nonce:   2,
					balance: uint256.NewInt(0),
				},
				{
					address: eoaMaxNonce,
					nonce:   math.MaxUint64, // unchanged
					balance: uint256.NewInt(0),
				},
			},
		},
		{
			name: "insufficient_funds",
			op: &Op{
				Burn: map[common.Address]AccountDebit{
					eoa: {
						Amount:     *uint256.NewInt(1),
						MinBalance: *uint256.NewInt(1),
					},
				},
			},
			wantErr: core.ErrInsufficientFunds,
		},
		{
			name: "fund_eoa_for_min_balance_tests",
			op: &Op{
				Mint: map[common.Address]uint256.Int{
					eoa: *uint256.NewInt(500),
				},
			},
			wantAccounts: []account{
				{
					address: eoa,
					nonce:   2,
					balance: uint256.NewInt(500),
				},
			},
		},
		{
			name: "balance_below_min_balance",
			op: &Op{
				Burn: map[common.Address]AccountDebit{
					eoa: {
						Amount:     *uint256.NewInt(100),
						MinBalance: *uint256.NewInt(1000),
					},
				},
			},
			wantErr: core.ErrInsufficientFunds,
		},
		{
			name: "balance_covers_min_balance_debits_amount",
			op: &Op{
				Burn: map[common.Address]AccountDebit{
					eoa: {
						Amount:     *uint256.NewInt(100),
						MinBalance: *uint256.NewInt(500),
					},
				},
			},
			wantAccounts: []account{
				{
					address: eoa,
					nonce:   3,
					balance: uint256.NewInt(400),
				},
			},
		},
		{
			name: "min_balance_unset_does_not_allow_underflow",
			op: &Op{
				Burn: map[common.Address]AccountDebit{
					eoa: {
						Amount: *uint256.NewInt(500),
					},
				},
			},
			wantErr: errMinBalanceBelowAmount,
			wantAccounts: []account{
				{
					address: eoa,
					nonce:   3,
					balance: uint256.NewInt(400),
				},
			},
		},
		{
			name: "min_balance_below_amount_does_not_allow_underflow",
			op: &Op{
				Burn: map[common.Address]AccountDebit{
					eoa: {
						Amount:     *uint256.NewInt(500),
						MinBalance: *uint256.NewInt(300),
					},
				},
			},
			wantErr: errMinBalanceBelowAmount,
			wantAccounts: []account{
				{
					address: eoa,
					nonce:   3,
					balance: uint256.NewInt(400),
				},
			},
		},
	}
	for _, tt := range steps {
		require.ErrorIs(t, tt.op.ApplyTo(db), tt.wantErr, "ApplyTo %s", tt.name)
		for _, acct := range tt.wantAccounts {
			assert.Equalf(t, acct.nonce, db.GetNonce(acct.address), "nonce of account %s after %s", acct.address, tt.name)
			assert.Equalf(t, acct.balance, db.GetBalance(acct.address), "balance of account %s after %s", acct.address, tt.name)
		}
		if t.Failed() {
			t.FailNow()
		}
	}
}
