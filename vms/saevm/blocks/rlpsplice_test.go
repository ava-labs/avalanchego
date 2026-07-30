// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"
)

// TestSpliceBlockRLP guards the hook-alignment convention documented on
// [SpliceBlockRLP] under the default [types.BlockBodyHooks].
func TestSpliceBlockRLP(t *testing.T) {
	header := &types.Header{
		Number:     big.NewInt(2),
		Difficulty: big.NewInt(0),
		Extra:      []byte("header"),
	}
	uncle := &types.Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(0),
	}
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    1,
		Gas:      21_000,
		GasPrice: big.NewInt(2),
		Value:    big.NewInt(3),
		V:        big.NewInt(0),
		R:        big.NewInt(0),
		S:        big.NewInt(0),
	})
	populated := types.NewBlockWithHeader(header).WithBody(types.Body{
		Transactions: []*types.Transaction{tx},
		Uncles:       []*types.Header{uncle},
	})

	tests := []struct {
		name  string
		block *types.Block
	}{
		{"empty_body", types.NewBlockWithHeader(header)},
		{"transactions_and_uncles", populated},
		{"withdrawals", populated.WithWithdrawals(types.Withdrawals{{Index: 4, Validator: 5, Amount: 6}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headerRLP, err := rlp.EncodeToBytes(tt.block.Header())
			require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", tt.block.Header())
			bodyRLP, err := rlp.EncodeToBytes(tt.block.Body())
			require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", tt.block.Body())
			want, err := rlp.EncodeToBytes(tt.block)
			require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", tt.block)

			got, err := SpliceBlockRLP(headerRLP, bodyRLP)
			require.NoError(t, err, "SpliceBlockRLP()")
			require.Equal(t, want, got, "SpliceBlockRLP()")
		})
	}
}

func TestSpliceBlockRLPErrors(t *testing.T) {
	var (
		emptyList = rlp.RawValue{0xC0}
		emptyStr  = rlp.RawValue{0x80}
		trailing  = rlp.RawValue{0xC0, 0x00}
	)
	tests := []struct {
		name         string
		header, body rlp.RawValue
		wantErr      error
		wantContains string
	}{
		{"non_list_header", emptyStr, emptyList, rlp.ErrExpectedList, "splitting header"},
		{"header_trailing_bytes", trailing, emptyList, errTrailingBytes, "splitting header"},
		{"non_list_body", emptyList, emptyStr, rlp.ErrExpectedList, "splitting body"},
		{"body_trailing_bytes", emptyList, trailing, errTrailingBytes, "splitting body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SpliceBlockRLP(tt.header, tt.body)
			require.ErrorIsf(t, err, tt.wantErr, "SpliceBlockRLP(%#x, %#x)", tt.header, tt.body)
			require.ErrorContainsf(t, err, tt.wantContains, "SpliceBlockRLP(%#x, %#x)", tt.header, tt.body)
		})
	}
}

func TestAppendListHeader(t *testing.T) {
	for _, size := range []int{0, 1, 55, 56, 255, 256, 65_535, 65_536} {
		buf := appendListHeader(nil, size)
		buf = append(buf, make([]byte, size)...)
		content, rest, err := rlp.SplitList(buf)
		require.NoErrorf(t, err, "rlp.SplitList() of %d zero bytes wrapped by appendListHeader()", size)
		require.Emptyf(t, rest, "trailing bytes after list with %d-byte payload", size)
		require.Lenf(t, content, size, "payload of list declared to hold %d bytes", size)
	}
}
