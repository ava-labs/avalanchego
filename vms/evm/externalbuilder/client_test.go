// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package externalbuilder

import (
	"context"
	"errors"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"
)

// sidecar serves [Method] with a fixed response, recording the header it
// receives.
type sidecar struct {
	txs    []hexutil.Bytes
	err    error
	header *types.Header
}

func (s *sidecar) ProposeTxs(_ context.Context, hdr *types.Header) ([]hexutil.Bytes, error) {
	s.header = hdr
	return s.txs, s.err
}

func dial(t *testing.T, s *sidecar) *Client {
	t.Helper()

	srv := rpc.NewServer()
	require.NoError(t, srv.RegisterName("builder", s), "RegisterName()")
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(func() {
		httpSrv.Close()
		srv.Stop()
	})

	c, err := Dial(t.Context(), httpSrv.URL)
	require.NoError(t, err, "Dial()")
	t.Cleanup(c.Close)
	return c
}

func TestProposeTxs(t *testing.T) {
	hdr := &types.Header{
		ParentHash: common.Hash{1},
		Number:     big.NewInt(7),
		Difficulty: big.NewInt(1),
		GasLimit:   15_000_000,
		BaseFee:    big.NewInt(25),
	}
	want := []*types.Transaction{
		types.NewTx(&types.LegacyTx{Nonce: 1, To: &common.Address{2}, Gas: 21_000}),
		types.NewTx(&types.DynamicFeeTx{Nonce: 2, To: &common.Address{3}, Gas: 21_000}),
	}
	encoded := make([]hexutil.Bytes, len(want))
	for i, tx := range want {
		b, err := tx.MarshalBinary()
		require.NoError(t, err, "MarshalBinary()")
		encoded[i] = b
	}

	t.Run("transactions", func(t *testing.T) {
		s := &sidecar{txs: encoded}
		got, err := dial(t, s).ProposeTxs(t.Context(), hdr)
		require.NoError(t, err)
		require.Len(t, got, len(want))
		for i := range want {
			require.Equalf(t, want[i].Hash(), got[i].Hash(), "tx %d", i)
		}
		require.Equal(t, hdr.Hash(), s.header.Hash(), "header received by the sidecar")
	})

	t.Run("no transactions", func(t *testing.T) {
		got, err := dial(t, &sidecar{}).ProposeTxs(t.Context(), hdr)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("sidecar error", func(t *testing.T) {
		_, err := dial(t, &sidecar{err: errors.New("no bids")}).ProposeTxs(t.Context(), hdr)
		require.ErrorContains(t, err, "no bids")
	})

	t.Run("undecodable transaction", func(t *testing.T) {
		_, err := dial(t, &sidecar{txs: []hexutil.Bytes{{0x02, 0xff}}}).ProposeTxs(t.Context(), hdr)
		require.ErrorContains(t, err, "decoding tx 0")
	})
}
