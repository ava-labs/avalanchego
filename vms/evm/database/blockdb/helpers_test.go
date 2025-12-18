// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/utils/logging"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
)

func newDatabasesFromDir(t *testing.T, dataDir string) (*Database, ethdb.Database) {
	t.Helper()

	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	evmDB := rawdb.NewDatabase(evmdb.New(base))
	db, _, err := New(
		base,
		evmDB,
		dataDir,
		false,
		heightindexdb.DefaultConfig(),
		logging.NoLog{},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	return db, evmDB
}

// addrFromTest returns a deterministic address derived from the test name and supplied salt.
func addrFromTest(t *testing.T, salt string) common.Address {
	t.Helper()
	h := crypto.Keccak256Hash([]byte(t.Name() + ":" + salt))
	return common.BytesToAddress(h.Bytes()[12:])
}

// createBlocksToAddr generates blocks with a single funded sender and a tx to the provided recipient.
func createBlocksToAddr(t *testing.T, numBlocks int, to common.Address) ([]*types.Block, []types.Receipts) {
	t.Helper()

	key1, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	addr1 := crypto.PubkeyToAddress(key1.PublicKey)
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(params.Ether)}},
	}
	engine := dummy.NewFaker()
	signer := types.LatestSigner(params.TestChainConfig)
	gap := uint64(10)
	db, blocks, receipts, err := core.GenerateChainWithGenesis(
		gspec, engine, numBlocks-1, gap, func(_ int, gen *core.BlockGen) {
			tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
				ChainID:   params.TestChainConfig.ChainID,
				Nonce:     gen.TxNonce(addr1),
				To:        &to,
				Gas:       500_000,
				GasTipCap: big.NewInt(1),
				GasFeeCap: big.NewInt(1),
			}), signer, key1)
			gen.AddTx(tx)
		})
	require.NoError(t, err)

	// add genesis block since generated blocks and receipts don't include it
	genHash := rawdb.ReadCanonicalHash(db, 0)
	genBlock := rawdb.ReadBlock(db, genHash, 0)
	genReceipts := rawdb.ReadReceipts(db, genHash, 0, 0, params.TestChainConfig)
	blocks = slices.Concat([]*types.Block{genBlock}, blocks)
	receipts = slices.Concat([]types.Receipts{genReceipts}, receipts)

	return blocks, receipts
}

func createBlocks(t *testing.T, numBlocks int) ([]*types.Block, []types.Receipts) {
	t.Helper()
	to := addrFromTest(t, "default-to")
	return createBlocksToAddr(t, numBlocks, to)
}

func writeBlocks(db ethdb.KeyValueWriter, blocks []*types.Block, receipts []types.Receipts) {
	for i, block := range blocks {
		rawdb.WriteBlock(db, block)
		if i < len(receipts) {
			rawdb.WriteReceipts(db, block.Hash(), block.NumberU64(), receipts[i])
		}
		rawdb.WriteCanonicalHash(db, block.Hash(), block.NumberU64())
	}
}

func requireRLPEqual(t *testing.T, expected, actual interface{}) {
	t.Helper()
	expectedBytes, err := rlp.EncodeToBytes(expected)
	require.NoError(t, err)
	actualBytes, err := rlp.EncodeToBytes(actual)
	require.NoError(t, err)
	require.Equal(t, expectedBytes, actualBytes)
}

func logsFromReceipts(receipts types.Receipts) [][]*types.Log {
	logs := make([][]*types.Log, len(receipts))
	for i := range receipts {
		logs[i] = receipts[i].Logs
	}
	return logs
}
