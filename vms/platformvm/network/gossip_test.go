// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"

	pmempool "github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var errFoo = errors.New("foo")

// Add should error if verification errors
func TestMempoolWithVerificationAddVerificationError(t *testing.T) {
	require := require.New(t)

	txID := ids.GenerateTestID()
	tx := &txs.Tx{
		TxID: txID,
	}

	mempool, err := pmempool.New(
		"",
		gas.Dimensions{},
		1_000_000,
		ids.ID{},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	txVerifier := testTxVerifier{err: errFoo}

	mempoolWithVerification := newMempoolWithVerification(
		mempool,
		logging.NoLog{},
		txVerifier,
	)

	require.ErrorIs(mempoolWithVerification.Add(tx), errFoo)
	require.ErrorIs(mempoolWithVerification.GetDropReason(txID), errFoo)
}

// Adding a duplicate to the mempool should return an error
func TestMempoolWithVerificationAddDuplicate(t *testing.T) {
	require := require.New(t)

	testMempool, err := pmempool.New(
		"",
		gas.Dimensions{1, 1, 1, 1},
		1_000_000,
		snowtest.AVAXAssetID,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	txVerifier := testTxVerifier{}

	txID := ids.GenerateTestID()
	tx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: avax.BaseTx{
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID: ids.GenerateTestID(),
						},
						Asset: avax.Asset{
							ID: snowtest.AVAXAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 1,
						},
					},
				},
			},
		},
		TxID: txID,
	}

	require.NoError(testMempool.Add(tx))
	mempoolWithVerification := newMempoolWithVerification(
		testMempool,
		logging.NoLog{},
		txVerifier,
	)
	require.ErrorIs(mempoolWithVerification.Add(tx), mempool.ErrDuplicateTx)
}
