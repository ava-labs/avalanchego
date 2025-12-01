// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"crypto/ecdsa"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/core/txpool"
	"github.com/ava-labs/avalanchego/graft/coreth/core/txpool/legacypool"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
)

func TestGossipEthTxMarshaller(t *testing.T) {
	require := require.New(t)

	blobTx := &types.BlobTx{}
	want := &gossipTx{tx: types.NewTx(blobTx)}
	marshaller := gossipTxMarshaller{}

	bytes, err := marshaller.MarshalGossip(want)
	require.NoError(err)

	got, err := marshaller.UnmarshalGossip(bytes)
	require.NoError(err)
	require.Equal(want.GossipID(), got.GossipID())
}

func setupPoolWithConfig(t *testing.T, config *params.ChainConfig, fundedAddress common.Address) *txpool.TxPool {
	diskdb := rawdb.NewMemoryDatabase()
	engine := dummy.NewETHFaker()

	gspec := &core.Genesis{
		Config: config,
		Alloc:  types.GenesisAlloc{fundedAddress: {Balance: big.NewInt(1000000000000000000)}},
	}
	chain, err := core.NewBlockChain(diskdb, core.DefaultCacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
	require.NoError(t, err)
	testTxPoolConfig := legacypool.DefaultConfig
	legacyPool := legacypool.New(testTxPoolConfig, chain)

	txPool, err := txpool.New(testTxPoolConfig.PriceLimit, chain, []txpool.SubPool{legacyPool})
	require.NoError(t, err)

	return txPool
}

func getValidEthTxs(key *ecdsa.PrivateKey, count int, gasPrice *big.Int) []*types.Transaction {
	res := make([]*types.Transaction, count)

	to := common.Address{}
	amount := big.NewInt(0)
	gasLimit := uint64(37000)

	for i := 0; i < count; i++ {
		tx, _ := types.SignTx(
			types.NewTransaction(
				uint64(i),
				to,
				amount,
				gasLimit,
				gasPrice,
				[]byte(strings.Repeat("aaaaaaaaaa", 100))),
			types.HomesteadSigner{}, key)
		tx.SetTime(time.Now().Add(-1 * time.Minute))
		res[i] = tx
	}
	return res
}
