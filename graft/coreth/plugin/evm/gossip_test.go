// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/core/txpool/legacypool"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGossipEthTxMarshaller(t *testing.T) {
	require := require.New(t)

	blobTx := &types.BlobTx{}
	want := &GossipEthTx{Tx: types.NewTx(blobTx)}
	marshaller := GossipEthTxMarshaller{}

	bytes, err := marshaller.MarshalGossip(want)
	require.NoError(err)

	got, err := marshaller.UnmarshalGossip(bytes)
	require.NoError(err)
	require.Equal(want.GossipID(), got.GossipID())
}

func TestGossipSubscribe(t *testing.T) {
	require := require.New(t)
	key, err := crypto.GenerateKey()
	require.NoError(err)
	addr := crypto.PubkeyToAddress(key.PublicKey)

	require.NoError(err)
	txPool := setupPoolWithConfig(t, params.TestChainConfig, addr)
	defer txPool.Close()
	txPool.SetGasTip(common.Big1)
	txPool.SetMinFee(common.Big0)

	gossipTxPool, err := NewGossipEthTxPool(txPool, prometheus.NewRegistry())
	require.NoError(err)

	// use a custom bloom filter to test the bloom filter reset
	gossipTxPool.bloom, err = gossip.NewBloomFilter(prometheus.NewRegistry(), "", 1, 0.01, 0.0000000000000001) // maxCount =1
	require.NoError(err)
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	go gossipTxPool.Subscribe(ctx)

	require.Eventually(func() bool {
		return gossipTxPool.IsSubscribed()
	}, 10*time.Second, 500*time.Millisecond, "expected gossipTxPool to be subscribed")

	// create eth txs
	ethTxs := getValidEthTxs(key, 10, big.NewInt(226*utils.GWei))

	// Notify mempool about txs
	errs := txPool.AddRemotesSync(ethTxs)
	for _, err := range errs {
		require.NoError(err, "failed adding tx to remote mempool")
	}

	require.EventuallyWithTf(
		func(c *assert.CollectT) {
			gossipTxPool.lock.RLock()
			defer gossipTxPool.lock.RUnlock()

			for i, tx := range ethTxs {
				assert.Truef(c, gossipTxPool.bloom.Has(&GossipEthTx{Tx: tx}), "expected tx[%d] to be in bloom filter", i)
			}
		},
		30*time.Second,
		500*time.Millisecond,
		"expected all transactions to eventually be in the bloom filter",
	)
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
