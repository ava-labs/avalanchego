// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ TxVerifier = (*testVerifier)(nil)

type testVerifier struct {
	err error
}

func (v testVerifier) VerifyTx(*txs.Tx) error {
	return v.err
}

func TestMarshaller(t *testing.T) {
	require := require.New(t)

	parser, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	require.NoError(err)

	marhsaller := txParser{
		parser: parser,
	}

	want := &txs.Tx{Unsigned: &txs.BaseTx{}}
	require.NoError(want.Initialize(parser.Codec()))

	bytes, err := marhsaller.MarshalGossip(want)
	require.NoError(err)

	got, err := marhsaller.UnmarshalGossip(bytes)
	require.NoError(err)
	require.Equal(want.GossipID(), got.GossipID())
}

func TestMempoolWithVerificationAdd(t *testing.T) {
	require := require.New(t)

	metrics := prometheus.NewRegistry()

	baseMempool, err := mempool.New("", metrics)
	require.NoError(err)

	mempool := newMempoolWithVerification(baseMempool, testVerifier{})
	tx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{},
			},
		},
		TxID: ids.GenerateTestID(),
	}

	require.NoError(mempool.Add(tx))
	require.True(mempool.Has(tx.ID()))
}

func TestMempoolWithVerificationAddWithoutVerification(t *testing.T) {
	require := require.New(t)

	metrics := prometheus.NewRegistry()

	baseMempool, err := mempool.New("", metrics)
	require.NoError(err)

	mempool := newMempoolWithVerification(baseMempool, testVerifier{
		err: errors.New("verification failed"),
	})
	tx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{},
			},
		},
		TxID: ids.GenerateTestID(),
	}

	require.NoError(mempool.AddWithoutVerification(tx))
	require.True(mempool.Has(tx.ID()))
}
