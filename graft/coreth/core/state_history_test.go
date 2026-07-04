// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	ethparams "github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/graft/evm/firewood"
	"github.com/ava-labs/avalanchego/graft/evm/firewood/statehistory"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

// TestStateHistoryCapturesGenesisAndAcceptedBlocks pins that with
// StateHistoryEnabled the genesis commit seeds the history baseline at block 0
// naturally (it flows through the same propose/commit path as any block), and
// that every accepted block extends the contiguous captured range, readable
// through the overlay at each height.
func TestStateHistoryCapturesGenesisAndAcceptedBlocks(t *testing.T) {
	require := require.New(t)

	var (
		key1, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _  = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1    = crypto.PubkeyToAddress(key1.PublicKey)
		addr2    = crypto.PubkeyToAddress(key2.PublicKey)
		transfer = big.NewInt(10000)
	)

	gspec := &Genesis{
		Config:  params.TestChainConfig,
		Alloc:   types.GenesisAlloc{addr1: {Balance: new(big.Int).Mul(big.NewInt(100), big.NewInt(params.Ether))}},
		BaseFee: big.NewInt(ap3.InitialBaseFee),
	}
	cacheConfig := &CacheConfig{
		TrieCleanLimit:            256,
		TrieDirtyLimit:            256,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 4,
		Pruning:                   false,
		StateHistory:              32, // Firewood's minimum revision count
		CommitInterval:            16,
		SnapshotLimit:             0,
		AcceptorQueueLimit:        64,
		StateScheme:               customrawdb.FirewoodScheme,
		StateHistoryEnabled:       true,
		ChainDataDir:              t.TempDir(),
	}

	blockchain, err := createBlockChain(rawdb.NewMemoryDatabase(), cacheConfig, gspec, common.Hash{})
	require.NoError(err)
	defer blockchain.Stop()

	store := blockchain.TrieDB().Backend().(*firewood.TrieDB).HistoryStore()
	require.NotNil(store)

	// The genesis commit alone must have established the baseline.
	first, ok, err := store.FirstBlock()
	require.NoError(err)
	require.True(ok)
	require.Zero(first)

	signer := types.LatestSigner(gspec.Config)
	_, blocks, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 3, 10, func(_ int, gen *BlockGen) {
		tx, err := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, transfer, ethparams.TxGas, big.NewInt(ap3.InitialBaseFee), nil), signer, key1)
		require.NoError(err)
		gen.AddTx(tx)
	})
	require.NoError(err)

	_, err = blockchain.InsertChain(blocks)
	require.NoError(err)
	for _, b := range blocks {
		require.NoError(blockchain.Accept(b))
	}
	blockchain.DrainAcceptorQueue()

	head, ok, err := store.Head()
	require.NoError(err)
	require.True(ok)
	require.Equal(uint64(3), head)

	// addr2's balance at each height, served from the flat history.
	for target, want := range map[uint64]*uint256.Int{
		0: uint256.NewInt(0),
		1: uint256.NewInt(10000),
		2: uint256.NewInt(20000),
		3: uint256.NewInt(30000),
	} {
		header := blockchain.GetHeaderByNumber(target)
		require.NotNil(header)
		overlay := statehistory.NewOverlay(store, blockchain.StateCache(), target, header.Root)
		sdb, err := state.New(header.Root, overlay, nil)
		require.NoError(err)
		require.Equal(want, sdb.GetBalance(addr2), "target=%d", target)
	}
}
