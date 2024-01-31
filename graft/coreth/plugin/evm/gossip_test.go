// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestGossipAtomicTxMarshaller(t *testing.T) {
	require := require.New(t)

	want := &GossipAtomicTx{
		Tx: &Tx{
			UnsignedAtomicTx: &UnsignedImportTx{},
			Creds:            []verify.Verifiable{},
		},
	}
	marshaller := GossipAtomicTxMarshaller{}

	key0 := testKeys[0]
	require.NoError(want.Tx.Sign(Codec, [][]*secp256k1.PrivateKey{{key0}}))

	bytes, err := marshaller.MarshalGossip(want)
	require.NoError(err)

	got, err := marshaller.UnmarshalGossip(bytes)
	require.NoError(err)
	require.Equal(want.GossipID(), got.GossipID())
}

func TestAtomicMempoolIterate(t *testing.T) {
	txs := []*GossipAtomicTx{
		{
			Tx: &Tx{
				UnsignedAtomicTx: &TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
		{
			Tx: &Tx{
				UnsignedAtomicTx: &TestUnsignedTx{
					IDV: ids.GenerateTestID(),
				},
			},
		},
	}

	tests := []struct {
		name           string
		add            []*GossipAtomicTx
		f              func(tx *GossipAtomicTx) bool
		possibleValues []*GossipAtomicTx
		expectedLen    int
	}{
		{
			name: "func matches nothing",
			add:  txs,
			f: func(*GossipAtomicTx) bool {
				return false
			},
			possibleValues: nil,
		},
		{
			name: "func matches all",
			add:  txs,
			f: func(*GossipAtomicTx) bool {
				return true
			},
			possibleValues: txs,
			expectedLen:    2,
		},
		{
			name: "func matches subset",
			add:  txs,
			f: func(tx *GossipAtomicTx) bool {
				return tx.Tx == txs[0].Tx
			},
			possibleValues: txs,
			expectedLen:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			m, err := NewMempool(&snow.Context{}, prometheus.NewRegistry(), 10, nil)
			require.NoError(err)

			for _, add := range tt.add {
				require.NoError(m.Add(add))
			}

			matches := make([]*GossipAtomicTx, 0)
			f := func(tx *GossipAtomicTx) bool {
				match := tt.f(tx)

				if match {
					matches = append(matches, tx)
				}

				return match
			}

			m.Iterate(f)

			require.Len(matches, tt.expectedLen)
			require.Subset(tt.possibleValues, matches)
		})
	}
}

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
	defer txPool.Stop()
	txPool.SetGasPrice(common.Big1)
	txPool.SetMinFee(common.Big0)

	gossipTxPool, err := NewGossipEthTxPool(txPool, prometheus.NewRegistry())
	require.NoError(err)

	// use a custom bloom filter to test the bloom filter reset
	gossipTxPool.bloom, err = gossip.NewBloomFilter(prometheus.NewRegistry(), "", 1, 0.01, 0.0000000000000001) // maxCount =1
	require.NoError(err)
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	go gossipTxPool.Subscribe(ctx)

	// create eth txs
	ethTxs := getValidEthTxs(key, 10, big.NewInt(226*params.GWei))

	// Notify mempool about txs
	errs := txPool.AddRemotesSync(ethTxs)
	for _, err := range errs {
		require.NoError(err, "failed adding subnet-evm tx to remote mempool")
	}

	require.Eventually(
		func() bool {
			gossipTxPool.lock.RLock()
			defer gossipTxPool.lock.RUnlock()

			for _, tx := range ethTxs {
				if !gossipTxPool.bloom.Has(&GossipEthTx{Tx: tx}) {
					return false
				}
			}
			return true
		},
		10*time.Second,
		10*time.Millisecond,
		"expected all transactions to eventually be in the bloom filter",
	)
}

func setupPoolWithConfig(t *testing.T, config *params.ChainConfig, fundedAddress common.Address) *txpool.TxPool {
	diskdb := rawdb.NewMemoryDatabase()
	engine := dummy.NewETHFaker()

	gspec := &core.Genesis{
		Config: config,
		Alloc:  core.GenesisAlloc{fundedAddress: core.GenesisAccount{Balance: big.NewInt(1000000000000000000)}},
	}
	chain, err := core.NewBlockChain(diskdb, core.DefaultCacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
	require.NoError(t, err)
	testTxPoolConfig := txpool.DefaultConfig
	testTxPoolConfig.Journal = ""
	pool := txpool.NewTxPool(testTxPoolConfig, config, chain)

	return pool
}
