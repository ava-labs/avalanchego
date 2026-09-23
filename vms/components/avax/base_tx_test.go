// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

func TestBaseTxVerify(t *testing.T) {
	ctx := &snow.Context{
		NetworkID: 1,
		ChainID:   ids.GenerateTestID(),
	}

	tests := []struct {
		name string
		tx   *BaseTx
		want error
	}{
		{
			name: "nil_tx",
			tx:   nil,
			want: ErrNilTx,
		},
		{
			name: "wrong_network_id",
			tx: &BaseTx{
				NetworkID:    ctx.NetworkID + 1,
				BlockchainID: ctx.ChainID,
			},
			want: ErrWrongNetworkID,
		},
		{
			name: "wrong_chain_id",
			tx: &BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ids.GenerateTestID(),
			},
			want: ErrWrongChainID,
		},
		{
			name: "memo_too_large",
			tx: &BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
				Memo:         make([]byte, MaxMemoSize+1),
			},
			want: ErrMemoTooLarge,
		},
		{
			name: "valid",
			tx: &BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tx.Verify(ctx)
			require.ErrorIs(t, got, tt.want)
		})
	}
}

func TestVerifyMemoFieldLength(t *testing.T) {
	tests := []struct {
		name            string
		memo            []byte
		isDurangoActive bool
		want            error
	}{
		{
			name:            "empty_memo_pre_durango",
			memo:            nil,
			isDurangoActive: false,
			want:            nil,
		},
		{
			name:            "non_empty_memo_pre_durango",
			memo:            []byte("memo"),
			isDurangoActive: false,
			want:            nil,
		},
		{
			name:            "empty_memo_post_durango",
			memo:            nil,
			isDurangoActive: true,
			want:            nil,
		},
		{
			name:            "non_empty_memo_post_durango",
			memo:            []byte("memo"),
			isDurangoActive: true,
			want:            ErrMemoTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifyMemoFieldLength(tt.memo, tt.isDurangoActive)
			require.ErrorIs(t, got, tt.want)
		})
	}
}
