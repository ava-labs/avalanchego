// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
)

// stubExternalBuilder proposes whatever its propose function returns and
// records the headers it is called with.
type stubExternalBuilder struct {
	propose func(context.Context, *types.Header) ([]*types.Transaction, error)

	mu      sync.Mutex
	headers []*types.Header
}

func (s *stubExternalBuilder) ProposeTxs(ctx context.Context, hdr *types.Header) ([]*types.Transaction, error) {
	s.mu.Lock()
	s.headers = append(s.headers, hdr)
	s.mu.Unlock()
	return s.propose(ctx, hdr)
}

func withExternalBuilder(b ExternalBuilder, timeout time.Duration) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.ExternalBuilder = b
		c.vmConfig.ExternalBuilderTimeout = timeout
	})
}

func txHashes(txs []*types.Transaction) []common.Hash {
	hs := make([]common.Hash, len(txs))
	for i, tx := range txs {
		hs[i] = tx.Hash()
	}
	return hs
}

// transfer returns a signed transfer from the account, with the given tip.
func (s *SUT) transfer(tb testing.TB, account int, tip int64) *types.Transaction {
	tb.Helper()
	return s.wallet.SetNonceAndSign(tb, account, &types.DynamicFeeTx{
		To:        &common.Address{1},
		Gas:       params.TxGas,
		GasFeeCap: big.NewInt(100),
		GasTipCap: big.NewInt(tip),
	})
}

func TestExternalBuilderProposalLeadsBlock(t *testing.T) {
	stub := &stubExternalBuilder{}
	ctx, sut := newSUT(t, 3, withExternalBuilder(stub, 0))

	// The mempool transaction pays the highest tip, but proposed transactions
	// still come first, in the proposed order.
	mempoolTx := sut.transfer(t, 0, 10)
	proposed := []*types.Transaction{sut.transfer(t, 2, 0), sut.transfer(t, 1, 0)}
	stub.propose = func(context.Context, *types.Header) ([]*types.Transaction, error) {
		return proposed, nil
	}
	sut.sendTxsAndWaitUntilPending(t, mempoolTx)

	built, err := sut.rawVM.blockBuilder.build(ctx, nil, sut.genesis)
	require.NoError(t, err, "blockBuilder.build()")
	want := txHashes(append(proposed, mempoolTx))
	require.Equal(t, want, txHashes(built.Transactions()), "proposed txs, then mempool txs")

	require.Len(t, stub.headers, 1, "ProposeTxs() calls")
	hdr := stub.headers[0]
	require.Equal(t, sut.genesis.Hash(), hdr.ParentHash, "header.ParentHash")
	require.Equal(t, sut.genesis.Height()+1, hdr.Number.Uint64(), "header.Number")
	require.NotZero(t, hdr.GasLimit, "header.GasLimit is the worst-case limit")
	require.NotNil(t, hdr.BaseFee, "header.BaseFee is the worst-case base fee")

	// Other validators rebuild the block from its transactions without
	// consulting an external builder.
	parsed, err := sut.ParseBlock(ctx, built.Bytes())
	require.NoError(t, err, "ParseBlock()")
	require.NoError(t, parsed.Verify(ctx), "Verify()")
	require.Len(t, stub.headers, 1, "ProposeTxs() calls after verification")
}

func TestExternalBuilderFallsBackToMempool(t *testing.T) {
	const (
		builderFailed = "External builder failed to propose transactions"
		rejected      = "Building block without external proposal"
	)
	tests := []struct {
		name    string
		timeout time.Duration
		propose func(*testing.T, *SUT, *types.Transaction) func(context.Context, *types.Header) ([]*types.Transaction, error)
		// wantWarning is the warning explaining why the proposal was not
		// used, or empty if there is nothing to warn about.
		wantWarning string
	}{
		{
			name:        "builder error",
			wantWarning: builderFailed,
			propose: func(*testing.T, *SUT, *types.Transaction) func(context.Context, *types.Header) ([]*types.Transaction, error) {
				return func(context.Context, *types.Header) ([]*types.Transaction, error) {
					return nil, errors.New("no bids")
				}
			},
		},
		{
			name:        "builder timeout",
			timeout:     50 * time.Millisecond,
			wantWarning: builderFailed,
			propose: func(*testing.T, *SUT, *types.Transaction) func(context.Context, *types.Header) ([]*types.Transaction, error) {
				return func(ctx context.Context, _ *types.Header) ([]*types.Transaction, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}
			},
		},
		{
			name: "empty proposal",
			propose: func(*testing.T, *SUT, *types.Transaction) func(context.Context, *types.Header) ([]*types.Transaction, error) {
				return func(context.Context, *types.Header) ([]*types.Transaction, error) {
					return nil, nil
				}
			},
		},
		{
			name:        "proposed tx with nonce gap",
			wantWarning: rejected,
			propose: func(t *testing.T, sut *SUT, _ *types.Transaction) func(context.Context, *types.Header) ([]*types.Transaction, error) {
				valid := sut.transfer(t, 1, 0)
				_ = sut.transfer(t, 2, 0) // never proposed
				gap := sut.transfer(t, 2, 0)
				return func(context.Context, *types.Header) ([]*types.Transaction, error) {
					return []*types.Transaction{valid, gap}, nil
				}
			},
		},
		{
			name:        "proposed tx below base fee",
			wantWarning: rejected,
			propose: func(t *testing.T, sut *SUT, _ *types.Transaction) func(context.Context, *types.Header) ([]*types.Transaction, error) {
				cheap := sut.wallet.SetNonceAndSign(t, 1, &types.DynamicFeeTx{
					To:  &common.Address{1},
					Gas: params.TxGas,
				})
				return func(context.Context, *types.Header) ([]*types.Transaction, error) {
					return []*types.Transaction{cheap}, nil
				}
			},
		},
		{
			name:        "unsigned proposed tx",
			wantWarning: rejected,
			propose: func(*testing.T, *SUT, *types.Transaction) func(context.Context, *types.Header) ([]*types.Transaction, error) {
				unsigned := types.NewTx(&types.DynamicFeeTx{
					To:        &common.Address{1},
					Gas:       params.TxGas,
					GasFeeCap: big.NewInt(100),
				})
				return func(context.Context, *types.Header) ([]*types.Transaction, error) {
					return []*types.Transaction{unsigned}, nil
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubExternalBuilder{}
			logs := loggingtest.NewRecorder(logging.Warn)
			ctx, sut := newSUT(t, 3, withExternalBuilder(stub, tt.timeout), options.Func[sutConfig](func(c *sutConfig) {
				c.logger = logs
			}))

			mempoolTx := sut.transfer(t, 0, 10)
			stub.propose = tt.propose(t, sut, mempoolTx)
			sut.sendTxsAndWaitUntilPending(t, mempoolTx)

			start := time.Now()
			built, err := sut.rawVM.blockBuilder.build(ctx, nil, sut.genesis)
			require.NoError(t, err, "blockBuilder.build()")
			require.Less(t, time.Since(start), 5*time.Second, "build must not wait on the builder past its timeout")
			require.Equal(t, txHashes([]*types.Transaction{mempoolTx}), txHashes(built.Transactions()), "block built from the mempool alone")

			var warnings []string
			for _, r := range logs.AtLeast(logging.Warn) {
				warnings = append(warnings, r.Msg)
			}
			if tt.wantWarning == "" {
				require.Empty(t, warnings, "warnings")
			} else {
				require.Equal(t, []string{tt.wantWarning}, warnings, "warnings")
			}
		})
	}
}

func TestExternalBuilderProposesMempoolTx(t *testing.T) {
	stub := &stubExternalBuilder{}
	ctx, sut := newSUT(t, 2, withExternalBuilder(stub, 0))

	// The proposal includes a transaction that is also in the mempool. It
	// is included once, in its proposed position.
	shared := sut.transfer(t, 0, 10)
	other := sut.transfer(t, 1, 20)
	stub.propose = func(context.Context, *types.Header) ([]*types.Transaction, error) {
		return []*types.Transaction{shared}, nil
	}
	sut.sendTxsAndWaitUntilPending(t, shared, other)

	built, err := sut.rawVM.blockBuilder.build(ctx, nil, sut.genesis)
	require.NoError(t, err, "blockBuilder.build()")
	require.Equal(t, txHashes([]*types.Transaction{shared, other}), txHashes(built.Transactions()))
}

func TestExternalBuilderRejectedProposalIsUndone(t *testing.T) {
	stub := &stubExternalBuilder{}
	logs := loggingtest.NewRecorder(logging.Warn)
	ctx, sut := newSUT(t, 3, withExternalBuilder(stub, 0), options.Func[sutConfig](func(c *sutConfig) {
		c.logger = logs
	}))

	// The first proposed tx is valid and is also in the mempool, but the
	// second is not, so the proposal is rejected after the first was applied
	// to the worst-case state. The first MUST then still be includable from
	// the mempool.
	valid := sut.transfer(t, 1, 10)
	_ = sut.transfer(t, 2, 0) // never sent
	gap := sut.transfer(t, 2, 0)
	stub.propose = func(context.Context, *types.Header) ([]*types.Transaction, error) {
		return []*types.Transaction{valid, gap}, nil
	}
	mempoolTx := sut.transfer(t, 0, 20)
	sut.sendTxsAndWaitUntilPending(t, mempoolTx, valid)

	built, err := sut.rawVM.blockBuilder.build(ctx, nil, sut.genesis)
	require.NoError(t, err, "blockBuilder.build()")
	require.Equal(t, txHashes([]*types.Transaction{mempoolTx, valid}), txHashes(built.Transactions()), "mempool txs by tip")
	require.Len(t, logs.AtLeast(logging.Warn), 1, "warnings")

	parsed, err := sut.ParseBlock(ctx, built.Bytes())
	require.NoError(t, err, "ParseBlock()")
	require.NoError(t, parsed.Verify(ctx), "Verify()")
}
