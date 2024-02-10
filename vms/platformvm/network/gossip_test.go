// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var errFoo = errors.New("foo")

// Add should error if verification errors
func TestGossipMempoolAddVerificationError(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	txID := ids.GenerateTestID()
	tx := &txs.Tx{
		TxID: txID,
	}

	mempool := mempool.NewMockMempool(ctrl)
	txVerifier := testTxVerifier{err: errFoo}

	mempool.EXPECT().Get(txID).Return(nil, false)
	mempool.EXPECT().GetDropReason(txID).Return(nil)
	mempool.EXPECT().MarkDropped(txID, errFoo)

	gossipMempool, err := newGossipMempool(
		mempool,
		prometheus.NewRegistry(),
		logging.NoLog{},
		txVerifier,
		testConfig.ExpectedBloomFilterElements,
		testConfig.ExpectedBloomFilterFalsePositiveProbability,
		testConfig.MaxBloomFilterFalsePositiveProbability,
	)
	require.NoError(err)

	err = gossipMempool.Add(tx)
	require.ErrorIs(err, errFoo)
	require.False(gossipMempool.bloom.Has(tx))
}

// Add should error if adding to the mempool errors
func TestGossipMempoolAddError(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	txID := ids.GenerateTestID()
	tx := &txs.Tx{
		TxID: txID,
	}

	txVerifier := testTxVerifier{}
	mempool := mempool.NewMockMempool(ctrl)

	mempool.EXPECT().Get(txID).Return(nil, false)
	mempool.EXPECT().GetDropReason(txID).Return(nil)
	mempool.EXPECT().Add(tx).Return(errFoo)
	mempool.EXPECT().MarkDropped(txID, errFoo).AnyTimes()

	gossipMempool, err := newGossipMempool(
		mempool,
		prometheus.NewRegistry(),
		logging.NoLog{},
		txVerifier,
		testConfig.ExpectedBloomFilterElements,
		testConfig.ExpectedBloomFilterFalsePositiveProbability,
		testConfig.MaxBloomFilterFalsePositiveProbability,
	)
	require.NoError(err)

	err = gossipMempool.Add(tx)
	require.ErrorIs(err, errFoo)
	require.False(gossipMempool.bloom.Has(tx))
}

// Adding a duplicate to the mempool should return an error
func TestMempoolDuplicate(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	testMempool := mempool.NewMockMempool(ctrl)
	txVerifier := testTxVerifier{}

	txID := ids.GenerateTestID()
	tx := &txs.Tx{
		TxID: txID,
	}

	testMempool.EXPECT().Get(txID).Return(tx, true)

	gossipMempool, err := newGossipMempool(
		testMempool,
		prometheus.NewRegistry(),
		logging.NoLog{},
		txVerifier,
		testConfig.ExpectedBloomFilterElements,
		testConfig.ExpectedBloomFilterFalsePositiveProbability,
		testConfig.MaxBloomFilterFalsePositiveProbability,
	)
	require.NoError(err)

	err = gossipMempool.Add(tx)
	require.ErrorIs(err, mempool.ErrDuplicateTx)
	require.False(gossipMempool.bloom.Has(tx))
}

// Adding a tx to the mempool should add it to the bloom filter
func TestGossipAddBloomFilter(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	txID := ids.GenerateTestID()
	tx := &txs.Tx{
		TxID: txID,
	}

	txVerifier := testTxVerifier{}
	mempool := mempool.NewMockMempool(ctrl)

	mempool.EXPECT().Get(txID).Return(nil, false)
	mempool.EXPECT().GetDropReason(txID).Return(nil)
	mempool.EXPECT().Add(tx).Return(nil)
	mempool.EXPECT().Len().Return(0)
	mempool.EXPECT().RequestBuildBlock(false)

	gossipMempool, err := newGossipMempool(
		mempool,
		prometheus.NewRegistry(),
		logging.NoLog{},
		txVerifier,
		testConfig.ExpectedBloomFilterElements,
		testConfig.ExpectedBloomFilterFalsePositiveProbability,
		testConfig.MaxBloomFilterFalsePositiveProbability,
	)
	require.NoError(err)

	require.NoError(gossipMempool.Add(tx))
	require.True(gossipMempool.bloom.Has(tx))
}
