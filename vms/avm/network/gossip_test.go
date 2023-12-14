// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/avm/block/executor"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ executor.Manager = (*testVerifier)(nil)

func TestGossipTxMarshal(t *testing.T) {
	require := require.New(t)

	parser, err := txs.NewParser([]fxs.Fx{&secp256k1fx.Fx{}})
	require.NoError(err)

	want := &gossipTx{
		tx: &txs.Tx{
			TxID: ids.GenerateTestID(),
		},
		parser: parser,
	}

	bytes, err := want.Marshal()
	require.NoError(err)

	got := &gossipTx{}
	require.NoError(got.Unmarshal(bytes))
	require.Equal(want.GetID(), got.GetID())
}

func TestGossipMempoolAddTx(t *testing.T) {
	require := require.New(t)

	metrics := prometheus.NewRegistry()
	toEngine := make(chan common.Message, 1)

	baseMempool, err := mempool.New("", metrics, toEngine)
	require.NoError(err)

	parser, err := txs.NewParser(nil)
	require.NoError(err)

	mempool, err := newGossipMempool(
		baseMempool,
		testVerifier{},
		parser,
		txGossipBloomMaxElements,
		txGossipBloomFalsePositiveProbability,
		txGossipBloomMaxFalsePositiveProbability,
	)
	require.NoError(err)

	tx := &gossipTx{
		tx: &txs.Tx{
			Unsigned: &txs.BaseTx{
				BaseTx: avax.BaseTx{
					Ins: []*avax.TransferableInput{},
				},
			},
			TxID: ids.GenerateTestID(),
		},
		parser: parser,
	}

	require.NoError(mempool.Add(tx))
	require.True(mempool.bloom.Has(tx))
}

type testVerifier struct {
	executor.Manager
	fail bool
}

func (testVerifier) VerifyTx(*txs.Tx) error {
	return nil
}
