// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

var (
	validPredicate      = predicate.New([]byte{0})
	invalidPredicate    = predicate.New([]byte{1})
	errInvalidPredicate = errors.New("invalid predicate")
)

// predicaters is a [predicate.Predicates] reporting a predicate for every
// address in the set.
type predicaters set.Set[common.Address]

func (p predicaters) HasPredicate(addr common.Address) bool {
	s := set.Set[common.Address](p)
	return s.Contains(addr)
}

// verify treats [validPredicate] as passing and everything else as failing.
func verify(_ common.Address, pred predicate.Predicate) error {
	if slices.Equal(pred, validPredicate) {
		return nil
	}
	return errInvalidPredicate
}

func newAccessListTx(accessList types.AccessList) *types.Transaction {
	return types.NewTx(&types.DynamicFeeTx{AccessList: accessList})
}

func TestVerifyBlockPredicates(t *testing.T) {
	var (
		addr0 = common.Address{0}
		addr1 = common.Address{1}

		validTx = newAccessListTx(types.AccessList{
			{Address: addr0, StorageKeys: validPredicate},
		})
		invalidTx = newAccessListTx(types.AccessList{
			{Address: addr0, StorageKeys: invalidPredicate},
		})
		mixedTx = newAccessListTx(types.AccessList{
			{Address: addr0, StorageKeys: validPredicate},
			{Address: addr1, StorageKeys: invalidPredicate},
			{Address: addr0, StorageKeys: invalidPredicate},
			{Address: addr0, StorageKeys: invalidPredicate},
			{Address: addr1, StorageKeys: validPredicate},
			{Address: addr1, StorageKeys: validPredicate},
			{Address: addr1, StorageKeys: invalidPredicate},
			{Address: addr1, StorageKeys: validPredicate},
		})
	)
	tests := []struct {
		name             string
		contracts        []common.Address
		haveBlockContext bool
		txs              []*types.Transaction
		want             predicate.BlockResults
		wantErr          error
	}{
		{
			name: "no_predicaters",
			txs:  []*types.Transaction{validTx},
		},
		{
			name:      "no_predicates",
			contracts: []common.Address{addr0},
			txs:       []*types.Transaction{newAccessListTx(nil)},
		},
		{
			name:      "filtered_predicates",
			contracts: []common.Address{addr0},
			txs: []*types.Transaction{newAccessListTx(types.AccessList{
				{Address: addr1, StorageKeys: validPredicate},
			})},
		},
		{
			name:      "no_block_context",
			contracts: []common.Address{addr0},
			txs:       []*types.Transaction{validTx},
			wantErr:   ErrNoBlockContext,
		},
		{
			name:             "one_tx_one_address_one_predicate",
			contracts:        []common.Address{addr0},
			haveBlockContext: true,
			txs:              []*types.Transaction{validTx},
			want: predicate.BlockResults{
				validTx.Hash(): {
					addr0: set.NewBits(),
				},
			},
		},
		{
			name:             "one_tx_one_address_one_invalid_predicate",
			contracts:        []common.Address{addr0},
			haveBlockContext: true,
			txs:              []*types.Transaction{invalidTx},
			want: predicate.BlockResults{
				invalidTx.Hash(): {
					addr0: set.NewBits(0),
				},
			},
		},
		{
			name:             "one_address_two_invalid_predicates",
			contracts:        []common.Address{addr0},
			haveBlockContext: true,
			txs: []*types.Transaction{newAccessListTx(types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
			})},
			want: predicate.BlockResults{
				types.NewTx(&types.DynamicFeeTx{AccessList: types.AccessList{
					{Address: addr0, StorageKeys: invalidPredicate},
					{Address: addr0, StorageKeys: invalidPredicate},
				}}).Hash(): {
					addr0: set.NewBits(0, 1),
				},
			},
		},
		{
			name:             "two_addresses_mixed_predicates",
			contracts:        []common.Address{addr0, addr1},
			haveBlockContext: true,
			txs:              []*types.Transaction{mixedTx},
			want: predicate.BlockResults{
				mixedTx.Hash(): {
					addr0: set.NewBits(1, 2),
					addr1: set.NewBits(0, 3),
				},
			},
		},
		{
			name:             "multiple_txs",
			contracts:        []common.Address{addr0},
			haveBlockContext: true,
			txs: []*types.Transaction{
				validTx,
				invalidTx,
			},
			want: predicate.BlockResults{
				validTx.Hash(): {
					addr0: set.NewBits(),
				},
				invalidTx.Hash(): {
					addr0: set.NewBits(0),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := VerifyBlockPredicates(
				predicaters(set.Of(test.contracts...)),
				test.haveBlockContext,
				verify,
				test.txs,
			)
			require.ErrorIs(t, err, test.wantErr, "VerifyBlockPredicates()")
			if test.wantErr != nil {
				return
			}
			require.Equal(t, test.want, got, "VerifyBlockPredicates()")
		})
	}
}
