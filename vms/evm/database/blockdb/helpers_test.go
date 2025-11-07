// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
)

var (
	key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	addr1   = crypto.PubkeyToAddress(key1.PublicKey)
	addr2   = crypto.PubkeyToAddress(key2.PublicKey)
)

// blockingDatabase wraps a HeightIndex and blocks indefinitely on Put
// if shouldBlock is true.
type blockingDatabase struct {
	database.HeightIndex
	shouldBlock func() bool
}

func (b *blockingDatabase) Put(num uint64, encodedBlock []byte) error {
	if b.shouldBlock == nil || b.shouldBlock() {
		<-make(chan struct{})
	}
	return b.HeightIndex.Put(num, encodedBlock)
}

func newDatabasesFromDir(t *testing.T, dataDir string) (*Database, ethdb.Database) {
	t.Helper()

	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	kvDB := rawdb.NewDatabase(evmdb.New(base))
	db, _, err := New(base, kvDB, dataDir, false, heightindexdb.DefaultConfig(), logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)

	return db, kvDB
}

func createBlocks(t *testing.T, numBlocks int) ([]*types.Block, []types.Receipts) {
	t.Helper()

	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Number: 0,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(params.Ether)}},
	}
	engine := dummy.NewFaker()
	signer := types.LatestSigner(params.TestChainConfig)
	db, blocks, receipts, err := core.GenerateChainWithGenesis(
		gspec, engine, numBlocks-1, 10, func(_ int, gen *core.BlockGen) {
			tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
				ChainID:   params.TestChainConfig.ChainID,
				Nonce:     gen.TxNonce(addr1),
				To:        &addr2,
				Gas:       500_000,
				GasTipCap: big.NewInt(1),
				GasFeeCap: big.NewInt(1),
			}), signer, key1)
			gen.AddTx(tx)
		})
	require.NoError(t, err)

	// add genesis block
	genesisHash := rawdb.ReadCanonicalHash(db, 0)
	genesisBlock := rawdb.ReadBlock(db, genesisHash, 0)
	genesisReceipts := rawdb.ReadReceipts(db, genesisHash, 0, 0, params.TestChainConfig)
	blocks = slices.Concat([]*types.Block{genesisBlock}, blocks)
	receipts = slices.Concat([]types.Receipts{genesisReceipts}, receipts)

	return blocks, receipts
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

func startMigration(t *testing.T, db *Database, waitForCompletion bool) {
	t.Helper()

	db.migrator.completed.Store(false)
	require.NoError(t, db.StartMigration())

	if waitForCompletion {
		timeout := 5 * time.Second
		require.True(
			t, db.migrator.waitDone(timeout),
			"migration did not complete within timeout: %v", timeout,
		)
		require.True(t, db.migrator.isCompleted())
	}
}
