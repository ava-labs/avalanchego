// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customheader"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap4"

	ethparams "github.com/ava-labs/libevm/params"
)

var (
	TestCallbacks = dummy.ConsensusCallbacks{
		OnExtraStateChange: func(block *types.Block, _ *types.Header, statedb *state.StateDB) (*big.Int, *big.Int, error) {
			wrappedStateDB := extstate.New(statedb)
			wrappedStateDB.AddBalanceMultiCoin(common.HexToAddress("0xdeadbeef"), common.HexToHash("0xdeadbeef"), big.NewInt(block.Number().Int64()))
			return nil, nil, nil
		},
		OnFinalizeAndAssemble: func(
			header *types.Header,
			_ *types.Header,
			statedb *state.StateDB,
			_ []*types.Transaction,
		) ([]byte, *big.Int, *big.Int, error) {
			wrappedStateDB := extstate.New(statedb)
			wrappedStateDB.AddBalanceMultiCoin(common.HexToAddress("0xdeadbeef"), common.HexToHash("0xdeadbeef"), big.NewInt(header.Number.Int64()))
			return nil, nil, nil, nil
		},
	}
	TestEmptyCallbacks = dummy.ConsensusCallbacks{
		OnExtraStateChange: func(_ *types.Block, _ *types.Header, _ *state.StateDB) (*big.Int, *big.Int, error) {
			return nil, nil, nil
		},
		OnFinalizeAndAssemble: func(
			_ *types.Header,
			_ *types.Header,
			_ *state.StateDB,
			_ []*types.Transaction,
		) ([]byte, *big.Int, *big.Int, error) {
			return nil, nil, nil, nil
		},
	}
)

type (
	createFunc func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, dataPath string) (*BlockChain, error)
	ChainTest  struct {
		Name     string
		testFunc func(
			t *testing.T,
			create createFunc,
		)
	}
)

var tests = []ChainTest{
	{
		"InsertChainAcceptSingleBlock",
		InsertChainAcceptSingleBlockTest,
	},
	{
		"InsertForkedChain",
		InsertLongForkedChainTest,
	},
	{
		"AcceptNonCanonicalBlock",
		AcceptNonCanonicalBlockTest,
	},
	{
		"SetPreferenceRewind",
		SetPreferenceRewindTest,
	},
	{
		"BuildOnVariousStages",
		BuildOnVariousStagesTest,
	},
	{
		"EmptyBlocks",
		EmptyBlocksTest,
	},
	{
		"EmptyAndNonEmptyBlocks",
		EmptyAndNonEmptyBlocksTest,
	},
	{
		"ReorgReInsert",
		ReorgReInsertTest,
	},
	{
		"AcceptBlockIdenticalStateRoot",
		AcceptBlockIdenticalStateRootTest,
	},
	{
		"ReprocessAcceptBlockIdenticalStateRoot",
		ReprocessAcceptBlockIdenticalStateRootTest,
	},
	{
		"GenerateChainInvalidBlockFee",
		GenerateChainInvalidBlockFeeTest,
	},
	{
		"InsertChainInvalidBlockFee",
		InsertChainInvalidBlockFeeTest,
	},
	{
		"InsertChainValidBlockFee",
		InsertChainValidBlockFeeTest,
	},
}

type ReexecTestFunc func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, dataPath string, commitInterval uint64) (*BlockChain, error)

type ReexecTest struct {
	Name     string
	testFunc func(
		t *testing.T,
		create ReexecTestFunc,
	)
}

var reexecTests = []ReexecTest{
	{
		"ReexecBlocks",
		ReexecBlocksTest,
	},
	{
		"ReexecMaxBlocks",
		ReexecMaxBlocksTest,
	},
	{
		"ReexecCorruptedState",
		ReexecCorruptedStateTest,
	},
}

func copyMemDB(db ethdb.Database) (ethdb.Database, error) {
	newDB := rawdb.NewMemoryDatabase()
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		if err := newDB.Put(iter.Key(), iter.Value()); err != nil {
			return nil, err
		}
	}

	return newDB, nil
}

// checkBlockChainState creates a new BlockChain instance and checks that exporting each block from
// genesis to last accepted from the original instance yields the same last accepted block and state
// root.
// Additionally, create another BlockChain instance from [originalDB] to ensure that BlockChain is
// persisted correctly through a restart.
func checkBlockChainState(
	t *testing.T,
	bc *BlockChain,
	gspec *Genesis,
	originalDB ethdb.Database,
	create createFunc,
	checkState func(sdb *state.StateDB) error,
) (*BlockChain, *BlockChain) {
	var (
		require           = require.New(t)
		lastAcceptedBlock = bc.LastConsensusAcceptedBlock()
		newDB             = rawdb.NewMemoryDatabase()
	)

	acceptedState, err := bc.StateAt(lastAcceptedBlock.Root())
	require.NoError(err)
	require.NoError(checkState(acceptedState), "Check state failed for original blockchain")

	oldChainDataDir := bc.CacheConfig().ChainDataDir // cacheConfig uses same reference in most tests
	newBlockChain, err := create(newDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err, "Failed to create new blockchain instance")
	defer newBlockChain.Stop()

	for i := uint64(1); i <= lastAcceptedBlock.NumberU64(); i++ {
		block := bc.GetBlockByNumber(i)
		require.NotNilf(block, "Failed to retrieve block by number %d from original chain", i)
		require.NoErrorf(newBlockChain.InsertBlock(block), "Failed to insert block %s:%d", block.Hash().Hex(), block.NumberU64())
		require.NoErrorf(newBlockChain.Accept(block), "Failed to accept block %s:%d", block.Hash().Hex(), block.NumberU64())
	}
	newBlockChain.DrainAcceptorQueue()

	newLastAcceptedBlock := newBlockChain.LastConsensusAcceptedBlock()
	require.Equal(lastAcceptedBlock.Hash(), newLastAcceptedBlock.Hash())

	// Check that the state of [newBlockChain] passes the check
	acceptedState, err = newBlockChain.StateAt(lastAcceptedBlock.Root())
	require.NoError(err)
	require.NoErrorf(checkState(acceptedState), "Check state failed for newly generated blockchain")

	// Copy the database over to prevent any issues when re-using [originalDB] after this call.
	originalDB, err = copyMemDB(originalDB)
	require.NoError(err)
	restartedChain, err := create(originalDB, gspec, lastAcceptedBlock.Hash(), oldChainDataDir)
	require.NoError(err)
	defer restartedChain.Stop()
	currentBlock := restartedChain.CurrentBlock()
	require.Equal(lastAcceptedBlock.Hash(), currentBlock.Hash(), "Restarted chain's current block does not match last accepted block")
	restartedLastAcceptedBlock := restartedChain.LastConsensusAcceptedBlock()
	require.Equal(lastAcceptedBlock.Hash(), restartedLastAcceptedBlock.Hash(), "Restarted chain's last accepted block does not match last accepted block")

	// Check that the state of [restartedChain] passes the check
	acceptedState, err = restartedChain.StateAt(lastAcceptedBlock.Root())
	require.NoError(err)
	require.NoError(checkState(acceptedState), "Check state failed for restarted blockchain")

	return newBlockChain, restartedChain
}

func InsertChainAcceptSingleBlockTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}
	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	// This call generates a chain of 3 blocks.
	signer := types.HomesteadSigner{}
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 3, 10, func(_ int, gen *BlockGen) {
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)

	// Insert three blocks into the chain and accept only the first block.
	_, err = blockchain.InsertChain(chain)
	require.NoError(err)
	require.NoError(blockchain.Accept(chain[0]))
	blockchain.DrainAcceptorQueue()

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce := sdb.GetNonce(addr1)
		if nonce != 1 {
			return fmt.Errorf("expected nonce addr1: 1, found nonce: %d", nonce)
		}
		transferredFunds := uint256.MustFromBig(big.NewInt(10000))
		balance1 := sdb.GetBalance(addr1)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance1 := new(uint256.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance1) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance: %d", expectedBalance1, balance1)
		}

		balance2 := sdb.GetBalance(addr2)
		expectedBalance2 := transferredFunds
		if balance2.Cmp(expectedBalance2) != 0 {
			return fmt.Errorf("expected addr2 balance: %d, found balance: %d", expectedBalance2, balance2)
		}

		nonce = sdb.GetNonce(addr2)
		if nonce != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce: %d", nonce)
		}
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

func InsertLongForkedChainTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	numBlocks := 129
	signer := types.HomesteadSigner{}
	_, chain1, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, numBlocks, 10, func(_ int, gen *BlockGen) {
		// Generate a transaction to create a unique block
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)
	// Generate the forked chain to be longer than the original chain to check for a regression where
	// a longer chain can trigger a reorg.
	_, chain2, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, numBlocks+1, 10, func(_ int, gen *BlockGen) {
		// Generate a transaction with a different amount to ensure [chain2] is different than [chain1].
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(5000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)

	if blockchain.snaps != nil {
		got := blockchain.snaps.NumBlockLayers()
		require.Equal(1, got, "incorrect snapshot layer count")
	}

	// Insert both chains.
	_, err = blockchain.InsertChain(chain1)
	require.NoError(err)

	if blockchain.snaps != nil {
		got := blockchain.snaps.NumBlockLayers()
		require.Equal(1+len(chain1), got, "incorrect snapshot layer count")
	}

	_, err = blockchain.InsertChain(chain2)
	require.NoError(err)

	if blockchain.snaps != nil {
		got := blockchain.snaps.NumBlockLayers()
		require.Equal(1+len(chain1)+len(chain2), got, "incorrect snapshot layer count")
	}

	currentBlock := blockchain.CurrentBlock()
	expectedCurrentBlock := chain1[len(chain1)-1]
	require.Equal(expectedCurrentBlock.Hash(), currentBlock.Hash())

	require.NoError(blockchain.ValidateCanonicalChain())

	// Accept the first block in [chain1], reject all blocks in [chain2] to
	// mimic the order that the consensus engine will call Accept/Reject in
	// and then Accept the rest of the blocks in [chain1].
	require.NoError(blockchain.Accept(chain1[0]))
	blockchain.DrainAcceptorQueue()

	if blockchain.snaps != nil {
		// Snap layer count should match chain length
		require.Equal(len(chain1)+len(chain2), blockchain.snaps.NumBlockLayers(), "incorrect snapshot layer count")
	}

	for i := 0; i < len(chain2); i++ {
		require.NoError(blockchain.Reject(chain2[i]))

		if blockchain.snaps != nil {
			// Snap layer count should decrease by 1 per Reject
			require.Equal(len(chain1)+len(chain2)-i-1, blockchain.snaps.NumBlockLayers(), "incorrect snapshot layer count")
		}
	}

	if blockchain.snaps != nil {
		require.Equal(len(chain1), blockchain.snaps.NumBlockLayers(), "incorrect snapshot layer count")
	}

	for i := 1; i < len(chain1); i++ {
		require.NoError(blockchain.Accept(chain1[i]))
		blockchain.DrainAcceptorQueue()

		if blockchain.snaps != nil {
			// Snap layer count should decrease by 1 per Accept
			require.Equal(len(chain1)-i, blockchain.snaps.NumBlockLayers(), "incorrect snapshot layer count")
		}
	}

	lastAcceptedBlock := blockchain.LastConsensusAcceptedBlock()
	expectedLastAcceptedBlock := chain1[len(chain1)-1]
	require.Equal(expectedLastAcceptedBlock.Hash(), lastAcceptedBlock.Hash())
	require.NoError(blockchain.ValidateCanonicalChain())

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce1 := sdb.GetNonce(addr1)
		if nonce1 != 129 {
			return fmt.Errorf("expected addr1 nonce: 129, found nonce %d", nonce1)
		}
		balance1 := sdb.GetBalance(addr1)
		transferredFunds := uint256.NewInt(129 * 10_000)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance := new(uint256.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance: %d", expectedBalance, balance1)
		}
		nonce2 := sdb.GetNonce(addr2)
		if nonce2 != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce: %d", nonce2)
		}
		balance2 := sdb.GetBalance(addr2)
		if balance2.Cmp(transferredFunds) != 0 {
			return fmt.Errorf("expected addr2 balance: %d, found balance: %d", transferredFunds, balance2)
		}
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

func AcceptNonCanonicalBlockTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		// We use two separate databases since GenerateChain commits the state roots to its underlying
		// database.
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	numBlocks := 3
	signer := types.HomesteadSigner{}
	_, chain1, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, numBlocks, 10, func(_ int, gen *BlockGen) {
		// Generate a transaction to create a unique block
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)
	_, chain2, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, numBlocks, 10, func(_ int, gen *BlockGen) {
		// Generate a transaction with a different amount to create a chain of blocks different from [chain1]
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(5000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)

	// Insert three blocks into the chain and accept only the first.
	_, err = blockchain.InsertChain(chain1)
	require.NoError(err)
	_, err = blockchain.InsertChain(chain2)
	require.NoError(err)

	currentBlock := blockchain.CurrentBlock()
	expectedCurrentBlock := chain1[len(chain1)-1]
	require.Equal(expectedCurrentBlock.Hash(), currentBlock.Hash())

	require.NoError(blockchain.ValidateCanonicalChain())

	// Accept the first block in [chain2], reject all blocks in [chain1] to
	// mimic the order that the consensus engine will call Accept/Reject in.
	require.NoError(blockchain.Accept(chain2[0]))
	blockchain.DrainAcceptorQueue()

	for i := 0; i < len(chain1); i++ {
		require.NoError(blockchain.Reject(chain1[i]))
		require.False(blockchain.HasBlock(chain1[i].Hash(), chain1[i].NumberU64()))
	}

	lastAcceptedBlock := blockchain.LastConsensusAcceptedBlock()
	expectedLastAcceptedBlock := chain2[0]
	require.Equal(expectedLastAcceptedBlock.Hash(), lastAcceptedBlock.Hash())
	require.NoError(blockchain.ValidateCanonicalChain())

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce1 := sdb.GetNonce(addr1)
		if nonce1 != 1 {
			return fmt.Errorf("expected addr1 nonce: 1, found nonce: %d", nonce1)
		}
		balance1 := sdb.GetBalance(addr1)
		transferredFunds := uint256.NewInt(5000)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance := new(uint256.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance) != 0 {
			return fmt.Errorf("expected balance1: %d, found balance: %d", expectedBalance, balance1)
		}
		nonce2 := sdb.GetNonce(addr2)
		if nonce2 != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce %d", nonce2)
		}
		balance2 := sdb.GetBalance(addr2)
		if balance2.Cmp(transferredFunds) != 0 {
			return fmt.Errorf("expected balance2: %d, found %d", transferredFunds, balance2)
		}
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

func SetPreferenceRewindTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	numBlocks := 3
	signer := types.HomesteadSigner{}
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, numBlocks, 10, func(_ int, gen *BlockGen) {
		// Generate a transaction to create a unique block
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)

	// Insert three blocks into the chain and accept only the first.
	_, err = blockchain.InsertChain(chain)
	require.NoError(err)

	currentBlock := blockchain.CurrentBlock()
	expectedCurrentBlock := chain[len(chain)-1]
	require.Equal(expectedCurrentBlock.Hash(), currentBlock.Hash())
	require.NoError(blockchain.ValidateCanonicalChain())

	// SetPreference to an ancestor of the currently preferred block. Test that this unlikely, but possible behavior
	// is handled correctly.
	require.NoError(blockchain.SetPreference(chain[0]))

	currentBlock = blockchain.CurrentBlock()
	expectedCurrentBlock = chain[0]
	require.Equal(expectedCurrentBlock.Hash(), currentBlock.Hash())

	lastAcceptedBlock := blockchain.LastConsensusAcceptedBlock()
	expectedLastAcceptedBlock := blockchain.Genesis()
	require.Equal(expectedLastAcceptedBlock.Hash(), lastAcceptedBlock.Hash())
	require.NoError(blockchain.ValidateCanonicalChain())

	// check the state of the last accepted block
	checkGenesisState := func(sdb *state.StateDB) error {
		nonce1 := sdb.GetNonce(addr1)
		if nonce1 != 0 {
			return fmt.Errorf("expected addr1 nonce: 0, found nonce: %d", nonce1)
		}
		balance1 := sdb.GetBalance(addr1)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		if balance1.Cmp(genesisBalance) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance: %d", genesisBalance, balance1)
		}
		nonce2 := sdb.GetNonce(addr2)
		if nonce2 != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce: %d", nonce2)
		}
		balance2 := sdb.GetBalance(addr2)
		if balance2.Cmp(common.U2560) != 0 {
			return fmt.Errorf("expected addr2 balance: 0, found balance %d", balance2)
		}
		return nil
	}
	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkGenesisState)

	require.NoError(blockchain.Accept(chain[0]))
	blockchain.DrainAcceptorQueue()

	lastAcceptedBlock = blockchain.LastConsensusAcceptedBlock()
	expectedLastAcceptedBlock = chain[0]
	require.Equal(expectedLastAcceptedBlock.Hash(), lastAcceptedBlock.Hash())
	require.NoError(blockchain.ValidateCanonicalChain())

	checkUpdatedState := func(sdb *state.StateDB) error {
		nonce := sdb.GetNonce(addr1)
		if nonce != 1 {
			return fmt.Errorf("expected addr1 nonce: 1, found nonce: %d", nonce)
		}
		transferredFunds := uint256.NewInt(10000)
		balance1 := sdb.GetBalance(addr1)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance1 := new(uint256.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance1) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance %d", expectedBalance1, balance1)
		}

		balance2 := sdb.GetBalance(addr2)
		expectedBalance2 := transferredFunds
		if balance2.Cmp(expectedBalance2) != 0 {
			return fmt.Errorf("expected addr2 balance: %d, found balance: %d", expectedBalance2, balance2)
		}

		nonce = sdb.GetNonce(addr2)
		if nonce != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce: %d", nonce)
		}
		return nil
	}
	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkUpdatedState)
}

func BuildOnVariousStagesTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		key3, _ = crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		addr3   = crypto.PubkeyToAddress(key3.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc: types.GenesisAlloc{
			addr1: {Balance: genesisBalance},
			addr3: {Balance: genesisBalance},
		},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	// This call generates a chain of 3 blocks.
	signer := types.HomesteadSigner{}
	genDB, chain1, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 20, 10, func(i int, gen *BlockGen) {
		// Send all funds back and forth between the two accounts
		if i%2 == 0 {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, genesisBalance, ethparams.TxGas, nil, nil), signer, key1)
			gen.AddTx(tx)
		} else {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr2), addr1, genesisBalance, ethparams.TxGas, nil, nil), signer, key2)
			gen.AddTx(tx)
		}
	})
	require.NoError(err)

	// Build second chain forked off of the 10th block in [chain1]
	chain2, _, err := GenerateChain(gspec.Config, chain1[9], blockchain.engine, genDB, 10, 10, func(i int, gen *BlockGen) {
		// Send all funds back and forth between the two accounts
		if i%2 == 0 {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr3), addr2, genesisBalance, ethparams.TxGas, nil, nil), signer, key3)
			gen.AddTx(tx)
		} else {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr2), addr3, genesisBalance, ethparams.TxGas, nil, nil), signer, key2)
			gen.AddTx(tx)
		}
	})
	require.NoError(err)

	// Build third chain forked off of the 5th block in [chain1].
	// The parent of this chain will be accepted before this fork
	// is inserted.
	chain3, _, err := GenerateChain(gspec.Config, chain1[4], blockchain.engine, genDB, 10, 10, func(i int, gen *BlockGen) {
		// Send all funds back and forth between accounts 2 and 3.
		if i%2 == 0 {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr2), addr3, genesisBalance, ethparams.TxGas, nil, nil), signer, key2)
			gen.AddTx(tx)
		} else {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr3), addr2, genesisBalance, ethparams.TxGas, nil, nil), signer, key3)
			gen.AddTx(tx)
		}
	})
	require.NoError(err)

	// Insert first 10 blocks from [chain1]
	_, err = blockchain.InsertChain(chain1)
	require.NoError(err)
	// Accept the first 5 blocks
	for _, block := range chain1[0:5] {
		require.NoError(blockchain.Accept(block))
	}
	blockchain.DrainAcceptorQueue()

	// Insert the forked chain [chain2] which starts at the 10th
	// block in [chain1] ie. a block that is still in processing.
	_, err = blockchain.InsertChain(chain2)
	require.NoError(err)
	// Insert another forked chain starting at the last accepted
	// block from [chain1].
	_, err = blockchain.InsertChain(chain3)
	require.NoError(err)
	// Accept the next block in [chain1] and then reject all
	// of the blocks in [chain3], which would then be rejected.
	require.NoError(blockchain.Accept(chain1[5]))
	blockchain.DrainAcceptorQueue()
	for _, block := range chain3 {
		require.NoError(blockchain.Reject(block))
	}
	// Accept the rest of the blocks in [chain1]
	for _, block := range chain1[6:10] {
		require.NoError(blockchain.Accept(block))
	}
	blockchain.DrainAcceptorQueue()

	// Accept the first block in [chain2] and reject the
	// subsequent blocks in [chain1] which would then be rejected.
	require.NoError(blockchain.Accept(chain2[0]))
	blockchain.DrainAcceptorQueue()

	for _, block := range chain1[10:] {
		require.NoError(blockchain.Reject(block))
	}

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce := sdb.GetNonce(addr1)
		if nonce != 5 {
			return fmt.Errorf("expected nonce addr1: 5, found nonce: %d", nonce)
		}
		balance1 := sdb.GetBalance(addr1)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance1 := genesisBalance
		if balance1.Cmp(expectedBalance1) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance: %d", expectedBalance1, balance1)
		}

		balance2 := sdb.GetBalance(addr2)
		expectedBalance2 := genesisBalance
		if balance2.Cmp(expectedBalance2) != 0 {
			return fmt.Errorf("expected addr2 balance: %d, found balance: %d", expectedBalance2, balance2)
		}

		nonce = sdb.GetNonce(addr2)
		if nonce != 5 {
			return fmt.Errorf("expected addr2 nonce: 5, found nonce: %d", nonce)
		}

		balance3 := sdb.GetBalance(addr3)
		expectedBalance3 := common.U2560
		if balance3.Cmp(expectedBalance3) != 0 {
			return fmt.Errorf("expected addr3 balance: %d, found balance: %d", expectedBalance3, balance3)
		}

		nonce = sdb.GetNonce(addr3)
		if nonce != 1 {
			return fmt.Errorf("expected addr3 nonce: 1, found nonce: %d", nonce)
		}
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

func EmptyBlocksTest(t *testing.T, create createFunc) {
	require := require.New(t)
	chainDB := rawdb.NewMemoryDatabase()
	// Ensure that key1 has some funds in the genesis block.
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 3, 10, func(_ int, _ *BlockGen) {})
	require.NoError(err)

	// Insert three blocks into the chain and accept only the first block.
	_, err = blockchain.InsertChain(chain)
	require.NoError(err)
	for _, block := range chain {
		require.NoError(blockchain.Accept(block))
	}
	blockchain.DrainAcceptorQueue()

	// Nothing to assert about the state
	checkState := func(*state.StateDB) error {
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

func EmptyAndNonEmptyBlocksTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 5, 10, func(i int, gen *BlockGen) {
		if i == 3 {
			// Generate a transaction to create a unique block
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), types.HomesteadSigner{}, key1)
			gen.AddTx(tx)
		}
	})
	require.NoError(err)

	_, err = blockchain.InsertChain(chain)
	require.NoError(err)
	for _, block := range chain {
		require.NoError(blockchain.Accept(block))
	}
	blockchain.DrainAcceptorQueue()

	// We expect results from block 3
	checkState := func(sdb *state.StateDB) error {
		nonce1 := sdb.GetNonce(addr1)
		if nonce1 != 1 {
			return fmt.Errorf("expected addr1 nonce: 3, found nonce: %d", nonce1)
		}
		balance1 := sdb.GetBalance(addr1)
		transferredFunds := uint256.NewInt(10000)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance := new(uint256.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance) != 0 {
			return fmt.Errorf("expected balance1: %d, found balance: %d", expectedBalance, balance1)
		}
		nonce2 := sdb.GetNonce(addr2)
		if nonce2 != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce %d", nonce2)
		}
		balance2 := sdb.GetBalance(addr2)
		if balance2.Cmp(transferredFunds) != 0 {
			return fmt.Errorf("expected balance2: %d, found %d", transferredFunds, balance2)
		}
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

func ReorgReInsertTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	signer := types.HomesteadSigner{}
	numBlocks := 3
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, numBlocks, 10, func(_ int, gen *BlockGen) {
		// Generate a transaction to create a unique block
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)

	// Insert and accept first block
	err = blockchain.InsertBlock(chain[0])
	require.NoError(err)
	require.NoError(blockchain.Accept(chain[0]))

	// Insert block and then set preference back (rewind) to last accepted blck
	err = blockchain.InsertBlock(chain[1])
	require.NoError(err)
	require.NoError(blockchain.SetPreference(chain[0]))

	// Re-insert and accept block
	err = blockchain.InsertBlock(chain[1])
	require.NoError(err)
	require.NoError(blockchain.Accept(chain[1]))

	// Build on top of the re-inserted block and accept
	err = blockchain.InsertBlock(chain[2])
	require.NoError(err)
	require.NoError(blockchain.Accept(chain[2]))
	blockchain.DrainAcceptorQueue()

	// Nothing to assert about the state
	checkState := func(sdb *state.StateDB) error {
		nonce1 := sdb.GetNonce(addr1)
		if nonce1 != 3 {
			return fmt.Errorf("expected addr1 nonce: 3, found nonce: %d", nonce1)
		}
		balance1 := sdb.GetBalance(addr1)
		transferredFunds := uint256.NewInt(30000)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance := new(uint256.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance) != 0 {
			return fmt.Errorf("expected balance1: %d, found balance: %d", expectedBalance, balance1)
		}
		nonce2 := sdb.GetNonce(addr2)
		if nonce2 != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce %d", nonce2)
		}
		balance2 := sdb.GetBalance(addr2)
		if balance2.Cmp(transferredFunds) != 0 {
			return fmt.Errorf("expected balance2: %d, found %d", transferredFunds, balance2)
		}
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

// Insert two different chains that result in the identical state root.
// Once we accept one of the chains, we insert and accept A3 on top of the shared
// state root
//
//	  G   (genesis)
//	 / \
//	A1  B1
//	|   |
//	A2  B2 (A2 and B2 represent two different paths to the identical state trie)
//	|
//	A3
//
//nolint:goimports
func AcceptBlockIdenticalStateRootTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	signer := types.HomesteadSigner{}
	_, chain1, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 3, 10, func(i int, gen *BlockGen) {
		if i < 2 {
			// Send half the funds from addr1 to addr2 in one transaction per each of the two blocks in [chain1]
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(500000000), ethparams.TxGas, nil, nil), signer, key1)
			gen.AddTx(tx)
		}
		// Allow the third block to be empty.
	})
	require.NoError(err)
	_, chain2, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 2, 10, func(i int, gen *BlockGen) {
		// Send 1/4 of the funds from addr1 to addr2 in tx1 and 3/4 of the funds in tx2. This will produce the identical state
		// root in the second block of [chain2] as is present in the second block of [chain1].
		if i == 0 {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(250000000), ethparams.TxGas, nil, nil), signer, key1)
			gen.AddTx(tx)
		} else {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(750000000), ethparams.TxGas, nil, nil), signer, key1)
			gen.AddTx(tx)
		}
	})
	require.NoError(err)

	// Assert that the block root of the second block in both chains is identical
	require.Equal(chain1[1].Root(), chain2[1].Root())

	// Insert first two blocks of [chain1] and both blocks in [chain2]
	// This leaves us one additional block to insert on top of [chain1]
	// after testing that the state roots are handled correctly.
	_, err = blockchain.InsertChain(chain1[:2])
	require.NoError(err)
	_, err = blockchain.InsertChain(chain2)
	require.NoError(err)

	currentBlock := blockchain.CurrentBlock()
	expectedCurrentBlock := chain1[1]
	require.Equal(expectedCurrentBlock.Hash(), currentBlock.Hash())

	// Accept the first block in [chain1] and reject all of [chain2]
	require.NoError(blockchain.Accept(chain1[0]))
	blockchain.DrainAcceptorQueue()

	for _, block := range chain2 {
		require.NoError(blockchain.Reject(block))
	}

	// Accept the last two blocks in [chain1]. This is a regression test to ensure
	// that we do not discard a snapshot difflayer that is still in use by a
	// processing block, when a different block with the same root is rejected.
	require.NoError(blockchain.Accept(chain1[1]))
	blockchain.DrainAcceptorQueue()

	lastAcceptedBlock := blockchain.LastConsensusAcceptedBlock()
	expectedLastAcceptedBlock := chain1[1]
	require.Equal(expectedLastAcceptedBlock.Hash(), lastAcceptedBlock.Hash())

	require.NoError(blockchain.InsertBlock(chain1[2]))
	require.NoError(blockchain.Accept(chain1[2]))
	blockchain.DrainAcceptorQueue()

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce1 := sdb.GetNonce(addr1)
		if nonce1 != 2 {
			return fmt.Errorf("expected addr1 nonce: 2, found nonce: %d", nonce1)
		}
		balance1 := sdb.GetBalance(addr1)
		expectedBalance := common.U2560
		if balance1.Cmp(expectedBalance) != 0 {
			return fmt.Errorf("expected balance1: %d, found balance: %d", expectedBalance, balance1)
		}
		nonce2 := sdb.GetNonce(addr2)
		if nonce2 != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce %d", nonce2)
		}
		balance2 := sdb.GetBalance(addr2)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		if balance2.Cmp(genesisBalance) != 0 {
			return fmt.Errorf("expected balance2: %d, found %d", genesisBalance, balance2)
		}
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

// Insert two different chains that result in the identical state root.
// Once we insert both of the chains, we restart, insert both the chains again,
// and then we accept one of the chains and accept A3 on top of the shared state
// root
//
//	  G   (genesis)
//	 / \
//	A1  B1
//	|   |
//	A2  B2 (A2 and B2 represent two different paths to the identical state trie)
//	|
//	A3
//
//nolint:goimports
func ReprocessAcceptBlockIdenticalStateRootTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	signer := types.HomesteadSigner{}
	_, chain1, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 3, 10, func(i int, gen *BlockGen) {
		if i < 2 {
			// Send half the funds from addr1 to addr2 in one transaction per each of the two blocks in [chain1]
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(500000000), ethparams.TxGas, nil, nil), signer, key1)
			gen.AddTx(tx)
		}
		// Allow the third block to be empty.
	})
	require.NoError(err)
	_, chain2, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 2, 10, func(i int, gen *BlockGen) {
		// Send 1/4 of the funds from addr1 to addr2 in tx1 and 3/4 of the funds in tx2. This will produce the identical state
		// root in the second block of [chain2] as is present in the second block of [chain1].
		if i == 0 {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(250000000), ethparams.TxGas, nil, nil), signer, key1)
			gen.AddTx(tx)
		} else {
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(750000000), ethparams.TxGas, nil, nil), signer, key1)
			gen.AddTx(tx)
		}
	})
	require.NoError(err)

	// Assert that the block root of the second block in both chains is identical
	require.Equal(chain1[1].Root(), chain2[1].Root())

	// Insert first two blocks of [chain1] and both blocks in [chain2]
	// This leaves us one additional block to insert on top of [chain1]
	// after testing that the state roots are handled correctly.
	_, err = blockchain.InsertChain(chain1[:2])
	require.NoError(err)
	_, err = blockchain.InsertChain(chain2)
	require.NoError(err)

	currentBlock := blockchain.CurrentBlock()
	expectedCurrentBlock := chain1[1]
	require.Equal(expectedCurrentBlock.Hash(), currentBlock.Hash())
	blockchain.Stop()

	// Restart the chain with a new database.
	chainDB = rawdb.NewMemoryDatabase()
	blockchain, err = create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	// Insert first two blocks of [chain1] and both blocks in [chain2]
	// This leaves us one additional block to insert on top of [chain1]
	// after testing that the state roots are handled correctly.
	_, err = blockchain.InsertChain(chain1[:2])
	require.NoError(err)
	_, err = blockchain.InsertChain(chain2)
	require.NoError(err)

	currentBlock = blockchain.CurrentBlock()
	expectedCurrentBlock = chain1[1]
	require.Equalf(expectedCurrentBlock.Hash(), currentBlock.Hash(), "block hash mismatch for expected height %d, actual height %d", expectedCurrentBlock.NumberU64(), currentBlock.Number.Uint64())

	// Accept the first block in [chain1] and reject all of [chain2]
	require.NoError(blockchain.Accept(chain1[0]))
	blockchain.DrainAcceptorQueue()

	for _, block := range chain2 {
		require.NoError(blockchain.Reject(block))
	}

	// Accept the last two blocks in [chain1]. This is a regression test to ensure
	// that we do not discard a snapshot difflayer that is still in use by a
	// processing block, when a different block with the same root is rejected.
	require.NoError(blockchain.Accept(chain1[1]))
	blockchain.DrainAcceptorQueue()

	lastAcceptedBlock := blockchain.LastConsensusAcceptedBlock()
	expectedLastAcceptedBlock := chain1[1]
	require.Equal(expectedLastAcceptedBlock.Hash(), lastAcceptedBlock.Hash())

	require.NoError(blockchain.InsertBlock(chain1[2]))
	require.NoError(blockchain.Accept(chain1[2]))
	blockchain.DrainAcceptorQueue()

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce1 := sdb.GetNonce(addr1)
		if nonce1 != 2 {
			return fmt.Errorf("expected addr1 nonce: 2, found nonce: %d", nonce1)
		}
		balance1 := sdb.GetBalance(addr1)
		expectedBalance := common.U2560
		if balance1.Cmp(expectedBalance) != 0 {
			return fmt.Errorf("expected balance1: %d, found balance: %d", expectedBalance, balance1)
		}
		nonce2 := sdb.GetNonce(addr2)
		if nonce2 != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce %d", nonce2)
		}
		balance2 := sdb.GetBalance(addr2)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		if balance2.Cmp(genesisBalance) != 0 {
			return fmt.Errorf("expected balance2: %d, found %d", genesisBalance, balance2)
		}
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

func GenerateChainInvalidBlockFeeTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := new(big.Int).Mul(big.NewInt(1000000), big.NewInt(params.Ether))
	gspec := &Genesis{
		Config: params.TestFortunaChainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	// This call generates a chain of 3 blocks.
	signer := types.LatestSigner(params.TestFortunaChainConfig)
	_, _, _, err = GenerateChainWithGenesis(gspec, blockchain.engine, 3, ap4.TargetBlockRate-1, func(_ int, gen *BlockGen) {
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   params.TestFortunaChainConfig.ChainID,
			Nonce:     gen.TxNonce(addr1),
			To:        &addr2,
			Gas:       ethparams.TxGas,
			GasFeeCap: gen.BaseFee(),
			GasTipCap: big.NewInt(0),
			Data:      []byte{},
		})

		signedTx, err := types.SignTx(tx, signer, key1)
		require.NoError(err)
		gen.AddTx(signedTx)
	})
	require.ErrorIs(err, customheader.ErrInsufficientBlockGas)
}

func InsertChainInvalidBlockFeeTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := new(big.Int).Mul(big.NewInt(1000000), big.NewInt(params.Ether))
	gspec := &Genesis{
		Config: params.TestFortunaChainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	// This call generates a chain of 3 blocks.
	signer := types.LatestSigner(params.TestFortunaChainConfig)
	eng := dummy.NewFakerWithMode(TestCallbacks, dummy.Mode{ModeSkipBlockFee: true, ModeSkipCoinbase: true})
	_, chain, _, err := GenerateChainWithGenesis(gspec, eng, 3, ap4.TargetBlockRate-1, func(_ int, gen *BlockGen) {
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   params.TestFortunaChainConfig.ChainID,
			Nonce:     gen.TxNonce(addr1),
			To:        &addr2,
			Gas:       ethparams.TxGas,
			GasFeeCap: gen.BaseFee(),
			GasTipCap: big.NewInt(0),
			Data:      []byte{},
		})

		signedTx, err := types.SignTx(tx, signer, key1)
		require.NoError(err)
		gen.AddTx(signedTx)
	})
	require.NoError(err)
	_, err = blockchain.InsertChain(chain)
	require.ErrorIs(err, customheader.ErrInsufficientBlockGas)
}

func InsertChainValidBlockFeeTest(t *testing.T, create createFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		// We use two separate databases since GenerateChain commits the state roots to its underlying
		// database.
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := new(big.Int).Mul(big.NewInt(1000000), big.NewInt(params.Ether))
	gspec := &Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir())
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	// This call generates a chain of 3 blocks.
	signer := types.LatestSigner(params.TestChainConfig)
	tip := big.NewInt(50000 * params.GWei)
	transfer := big.NewInt(10000)
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 3, ap4.TargetBlockRate-1, func(_ int, gen *BlockGen) {
		feeCap := new(big.Int).Add(gen.BaseFee(), tip)
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   params.TestChainConfig.ChainID,
			Nonce:     gen.TxNonce(addr1),
			To:        &addr2,
			Gas:       ethparams.TxGas,
			Value:     transfer,
			GasFeeCap: feeCap,
			GasTipCap: tip,
			Data:      []byte{},
		})

		signedTx, err := types.SignTx(tx, signer, key1)
		require.NoError(err)
		gen.AddTx(signedTx)
	})
	require.NoError(err)

	// Insert three blocks into the chain and accept only the first block.
	_, err = blockchain.InsertChain(chain)
	require.NoError(err)
	require.NoError(blockchain.Accept(chain[0]))
	blockchain.DrainAcceptorQueue()

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce := sdb.GetNonce(addr1)
		if nonce != 1 {
			return fmt.Errorf("expected nonce addr1: 1, found nonce: %d", nonce)
		}
		balance1 := sdb.GetBalance(addr1)
		transfer := uint256.MustFromBig(transfer)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance1 := new(uint256.Int).Sub(genesisBalance, transfer)
		baseFee := chain[0].BaseFee()
		feeSpend := new(big.Int).Mul(new(big.Int).Add(baseFee, tip), new(big.Int).SetUint64(ethparams.TxGas))
		expectedBalance1.Sub(expectedBalance1, uint256.MustFromBig(feeSpend))
		if balance1.Cmp(expectedBalance1) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance: %d", expectedBalance1, balance1)
		}

		balance2 := sdb.GetBalance(addr2)
		expectedBalance2 := transfer
		if balance2.Cmp(expectedBalance2) != 0 {
			return fmt.Errorf("expected addr2 balance: %d, found balance: %d", expectedBalance2, balance2)
		}

		nonce = sdb.GetNonce(addr2)
		if nonce != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce: %d", nonce)
		}
		return nil
	}

	checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)
}

func ReexecBlocksTest(t *testing.T, create ReexecTestFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir(), 4096)
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	// This call generates a chain of 10 blocks.
	signer := types.HomesteadSigner{}
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 10, 10, func(_ int, gen *BlockGen) {
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)

	// Insert three blocks into the chain and accept only the first block.
	_, err = blockchain.InsertChain(chain)
	require.NoError(err)

	foundTxs := []common.Hash{}
	missingTxs := []common.Hash{}
	for i, block := range chain {
		require.NoError(blockchain.Accept(block))

		if i == 3 {
			// At height 3, kill the async accepted block processor to force an
			// ungraceful recovery
			blockchain.stopAcceptor()
			blockchain.acceptorQueue = nil
		}

		if i <= 3 {
			// If <= height 3, all txs should be accessible on lookup
			for _, tx := range block.Transactions() {
				foundTxs = append(foundTxs, tx.Hash())
			}
		} else {
			// If > 3, all txs should be accessible on lookup
			for _, tx := range block.Transactions() {
				missingTxs = append(missingTxs, tx.Hash())
			}
		}
	}

	// After inserting all blocks, we should confirm that txs added after the
	// async worker shutdown cannot be found.
	for _, tx := range foundTxs {
		txLookup, _, _ := blockchain.GetTransactionLookup(tx)
		require.NotNilf(txLookup, "missing tx: %v", tx)
	}
	for _, tx := range missingTxs {
		txLookup, _, _ := blockchain.GetTransactionLookup(tx)
		require.Nilf(txLookup, "transaction should be missing: %v", tx)
	}

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce := sdb.GetNonce(addr1)
		if nonce != 10 {
			return fmt.Errorf("expected nonce addr1: 10, found nonce: %d", nonce)
		}
		transferredFunds := uint256.MustFromBig(big.NewInt(100000))
		balance1 := sdb.GetBalance(addr1)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance1 := new(uint256.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance1) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance: %d", expectedBalance1, balance1)
		}

		balance2 := sdb.GetBalance(addr2)
		expectedBalance2 := transferredFunds
		if balance2.Cmp(expectedBalance2) != 0 {
			return fmt.Errorf("expected addr2 balance: %d, found balance: %d", expectedBalance2, balance2)
		}

		nonce = sdb.GetNonce(addr2)
		if nonce != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce: %d", nonce)
		}
		return nil
	}

	// wrap the create function to set the commit interval
	checkCreate := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, dataPath string) (*BlockChain, error) {
		return create(db, gspec, lastAcceptedHash, dataPath, blockchain.cacheConfig.CommitInterval)
	}

	newChain, restartedChain := checkBlockChainState(t, blockchain, gspec, chainDB, checkCreate, checkState)

	allTxs := slices.Concat(foundTxs, missingTxs)
	for _, bc := range []*BlockChain{newChain, restartedChain} {
		// We should confirm that snapshots were properly initialized
		if bc.cacheConfig.SnapshotLimit > 0 {
			require.NotNil(bc.snaps, "snapshot initialization failed")
		}

		// We should confirm all transactions can now be queried
		for _, tx := range allTxs {
			txLookup, _, _ := bc.GetTransactionLookup(tx)
			require.NotNilf(txLookup, "missing tx: %v", tx)
		}
	}
}

func ReexecMaxBlocksTest(t *testing.T, create ReexecTestFunc) {
	var (
		require = require.New(t)
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, t.TempDir(), 4096)
	require.NoError(err)
	t.Cleanup(blockchain.Stop)

	// Check that we are generating enough blocks to test the reexec functionality.
	genNumBlocks := 20
	newCommitInterval := 6
	numAcceptedBlocks := 2*newCommitInterval - 1

	signer := types.HomesteadSigner{}
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, genNumBlocks, 10, func(_ int, gen *BlockGen) {
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)

	// Insert three blocks into the chain and accept only the first block.
	_, err = blockchain.InsertChain(chain)
	require.NoError(err)

	foundTxs := []common.Hash{}
	missingTxs := []common.Hash{}
	for i, block := range chain {
		require.NoError(blockchain.Accept(block))

		if i == numAcceptedBlocks {
			// kill the async accepted block processor to force an
			// ungraceful recovery
			blockchain.stopAcceptor()
			blockchain.acceptorQueue = nil
		}

		if i <= numAcceptedBlocks {
			// all txs should be accessible on lookup
			for _, tx := range block.Transactions() {
				foundTxs = append(foundTxs, tx.Hash())
			}
		} else {
			// all txs should be accessible on lookup
			for _, tx := range block.Transactions() {
				missingTxs = append(missingTxs, tx.Hash())
			}
		}
	}

	// After inserting all blocks, we should confirm that txs added after the
	// async worker shutdown cannot be found.
	for _, tx := range foundTxs {
		txLookup, _, _ := blockchain.GetTransactionLookup(tx)
		require.NotNilf(txLookup, "missing transaction: %v", tx)
	}
	for _, tx := range missingTxs {
		txLookup, _, _ := blockchain.GetTransactionLookup(tx)
		require.Nilf(txLookup, "transaction should be missing: %v", tx)
	}

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce := sdb.GetNonce(addr1)
		if nonce != uint64(genNumBlocks) {
			return fmt.Errorf("expected nonce addr1: %d, found nonce: %d", genNumBlocks, nonce)
		}
		transferredFunds := uint256.MustFromBig(big.NewInt(int64(10000 * genNumBlocks)))
		balance1 := sdb.GetBalance(addr1)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance1 := new(uint256.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance1) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance: %d", expectedBalance1, balance1)
		}

		balance2 := sdb.GetBalance(addr2)
		expectedBalance2 := transferredFunds
		if balance2.Cmp(expectedBalance2) != 0 {
			return fmt.Errorf("expected addr2 balance: %d, found balance: %d", expectedBalance2, balance2)
		}

		nonce = sdb.GetNonce(addr2)
		if nonce != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce: %d", nonce)
		}
		return nil
	}

	checkCreate := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, dataPath string) (*BlockChain, error) {
		return create(db, gspec, lastAcceptedHash, dataPath, uint64(newCommitInterval))
	}
	newChain, restartedChain := checkBlockChainState(t, blockchain, gspec, chainDB, checkCreate, checkState)

	allTxs := slices.Concat(foundTxs, missingTxs)
	for _, bc := range []*BlockChain{newChain, restartedChain} {
		// We should confirm that snapshots were properly initialized
		if bc.cacheConfig.SnapshotLimit > 0 {
			require.NotNil(bc.snaps, "snapshot initialization failed")
		}

		// We should confirm all transactions can now be queried
		for _, tx := range allTxs {
			txLookup, _, _ := bc.GetTransactionLookup(tx)
			require.NotNilf(txLookup, "missing transaction: %v", tx)
		}
	}
}

func ReexecCorruptedStateTest(t *testing.T, create ReexecTestFunc) {
	var (
		key1, _        = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _        = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1          = crypto.PubkeyToAddress(key1.PublicKey)
		addr2          = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB        = rawdb.NewMemoryDatabase()
		genesisBalance = big.NewInt(1000000)
		tempDir        = t.TempDir()
	)

	// Ensure that key1 has some funds in the genesis block.
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{}, tempDir, 4096)
	require.NoError(t, err)

	// Check that we are generating enough blocks to test the reexec functionality.
	signer := types.HomesteadSigner{}
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 10, 10, func(_ int, gen *BlockGen) {
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(t, err)

	// Insert three blocks into the chain and accept only the first block.
	_, err = blockchain.InsertChain(chain)
	require.NoError(t, err)

	// Accept only the first block.
	require.NoError(t, blockchain.Accept(chain[0]))

	// Simulate a crash by updating the acceptor tip
	blockchain.writeBlockAcceptedIndices(chain[1])
	blockchain.Stop()

	// Restart blockchain with existing state
	restartedBlockchain, err := create(chainDB, gspec, chain[1].Hash(), tempDir, 4096)
	require.NoError(t, err)
	defer restartedBlockchain.Stop()

	// We should be able to accept the remaining blocks
	for _, block := range chain[2:] {
		require.NoErrorf(t, restartedBlockchain.InsertBlock(block), "inserting block %d", block.NumberU64())
		require.NoErrorf(t, restartedBlockchain.Accept(block), "accepting block %d", block.NumberU64())
	}

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce := sdb.GetNonce(addr1)
		if nonce != 10 {
			return fmt.Errorf("expected nonce addr1: 10, found nonce: %d", nonce)
		}
		transferredFunds := uint256.MustFromBig(big.NewInt(100000))
		balance1 := sdb.GetBalance(addr1)
		genesisBalance := uint256.MustFromBig(genesisBalance)
		expectedBalance1 := new(uint256.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance1) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance: %d", expectedBalance1, balance1)
		}

		balance2 := sdb.GetBalance(addr2)
		expectedBalance2 := transferredFunds
		if balance2.Cmp(expectedBalance2) != 0 {
			return fmt.Errorf("expected addr2 balance: %d, found balance: %d", expectedBalance2, balance2)
		}

		nonce = sdb.GetNonce(addr2)
		if nonce != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce: %d", nonce)
		}
		return nil
	}

	// wrap the create function to set the commit interval
	checkCreate := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, dataPath string) (*BlockChain, error) {
		return create(db, gspec, lastAcceptedHash, dataPath, blockchain.cacheConfig.CommitInterval)
	}

	checkBlockChainState(t, restartedBlockchain, gspec, chainDB, checkCreate, checkState)
}
