// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

func TestParseEthBlock(t *testing.T) {
	zeroAddr := common.Address{}
	bodyWithTx := types.Body{
		Transactions: []*types.Transaction{
			types.NewTx(&types.DynamicFeeTx{
				To:        &zeroAddr,
				Gas:       params.TxGas,
				GasFeeCap: big.NewInt(1),
				Value:     big.NewInt(1),
			}),
		},
	}
	withdrawals := types.Withdrawals{&types.Withdrawal{Index: 1}}

	tests := []struct {
		name string
		// mutate will receive a valid header for an empty body and should return a mutated version of it.
		mutate      func(*types.Header) *types.Header
		body        types.Body
		withdrawals []*types.Withdrawal
		wantErr     error
	}{
		{
			name:   "valid_header", // base case for test setup
			mutate: func(h *types.Header) *types.Header { return h },
		},
		{
			name: "block_height_overflow_protection",
			mutate: func(h *types.Header) *types.Header {
				h.Number = new(big.Int).Lsh(big.NewInt(1), 64)
				return h
			},
			wantErr: errBlockHeightNotUint64,
		},
		{
			name: "invalid_tx_hash_empty",
			mutate: func(h *types.Header) *types.Header {
				h.TxHash = common.Hash{}
				return h
			},
			wantErr: errTxHashMismatch,
		},
		{
			name:    "invalid_tx_hash_nonempty",
			mutate:  func(h *types.Header) *types.Header { return h }, // uses [types.EmptyTxsHash]
			body:    bodyWithTx,                                       // contains a tx
			wantErr: errTxHashMismatch,
		},
		{
			name: "valid_tx_hash_nonempty",
			mutate: func(h *types.Header) *types.Header {
				h.TxHash = types.DeriveSha(types.Transactions(bodyWithTx.Transactions), saetest.TrieHasher())
				return h
			},
			body: bodyWithTx,
		},
		{
			name: "invalid_uncle_hash_empty",
			mutate: func(h *types.Header) *types.Header {
				h.UncleHash = common.Hash{}
				return h
			},
			wantErr: errUncleHashMismatch,
		},
		{
			name:   "invalid_uncle_hash_nonempty",
			mutate: func(h *types.Header) *types.Header { return h }, // uses [types.EmptyUncleHash]
			body: types.Body{
				Uncles: []*types.Header{{}},
			},
			wantErr: errUncleHashMismatch,
		},
		{
			name: "valid_uncle_hash_nonempty",
			mutate: func(h *types.Header) *types.Header {
				h.UncleHash = types.CalcUncleHash([]*types.Header{{}})
				return h
			},
			body: types.Body{
				Uncles: []*types.Header{{}},
			},
		},
		{
			name: "nil_withdrawals_nonnil_hash",
			mutate: func(h *types.Header) *types.Header {
				h.WithdrawalsHash = &types.EmptyWithdrawalsHash
				return h
			},
			wantErr: errWithdrawalHashMismatch,
		},
		{
			name: "nonnil_withdrawals_nil_hash",
			mutate: func(h *types.Header) *types.Header {
				h.WithdrawalsHash = nil
				return h
			},
			withdrawals: []*types.Withdrawal{},
			wantErr:     errWithdrawalHashMismatch,
		},
		{
			name: "nonnil_withdrawals_nonempty_hash",
			mutate: func(h *types.Header) *types.Header {
				h.WithdrawalsHash = &types.EmptyWithdrawalsHash
				return h
			},
			withdrawals: []*types.Withdrawal{},
		},
		{
			name: "valid_nonempty_withdrawals",
			mutate: func(h *types.Header) *types.Header {
				hash := types.DeriveSha(withdrawals, saetest.TrieHasher())
				h.WithdrawalsHash = &hash
				return h
			},
			withdrawals: withdrawals,
		},
		{
			name: "far_future_block_time",
			mutate: func(h *types.Header) *types.Header {
				h.Time = math.MaxInt64
				return h
			},
			wantErr: errBlockTooFarInFuture,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := loggingtest.New(t, logging.Debug)

			hdr := tt.mutate(&types.Header{
				Number:    big.NewInt(1),
				UncleHash: types.EmptyUncleHash,
				TxHash:    types.EmptyTxsHash,
			})
			ethB := types.NewBlockWithHeader(hdr).WithBody(tt.body).WithWithdrawals(tt.withdrawals)

			b, err := New(ethB, nil, nil, hooks(), log)
			require.NoError(t, err, "New()")
			_, err = ParseEth(b.Bytes(), hookstest.NewStub(0))
			assert.ErrorIs(t, err, tt.wantErr, "Parse(#%v @ time %v)", hdr.Number, hdr.Time)
		})
	}
}

// TestParseVerifyBlockSyntax verifies that [ParseEth] applies the hook-specific
// checks and propagates any error.
func TestParseVerifyBlockSyntax(t *testing.T) {
	log := loggingtest.New(t, logging.Debug)
	ethB := types.NewBlockWithHeader(&types.Header{
		Number:    big.NewInt(1),
		UncleHash: types.EmptyUncleHash,
		TxHash:    types.EmptyTxsHash,
	})
	b, err := New(ethB, nil, nil, hooks(), log)
	require.NoError(t, err, "New()")
	bytes := b.Bytes()

	errChainSpecific := errors.New("hook check rejected the block")
	tests := []struct {
		name    string
		verify  func(*types.Block) error
		wantErr error
	}{
		{
			name:   "hook_accepts",
			verify: func(*types.Block) error { return nil },
		},
		{
			name:    "hook_rejects",
			verify:  func(*types.Block) error { return errChainSpecific },
			wantErr: errChainSpecific,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hooks := hookstest.NewStub(0)
			hooks.VerifyBlockSyntaxFn = tt.verify
			_, err := ParseEth(bytes, hooks)
			assert.ErrorIs(t, err, tt.wantErr, "Parse()")
		})
	}
}
