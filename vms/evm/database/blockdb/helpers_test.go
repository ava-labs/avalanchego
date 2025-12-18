// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
)

// blockingDatabase wraps a HeightIndex and blocks indefinitely on Put
// if shouldBlock returns true.
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

// createBlocksToAddr generates blocks with receipts containing a log to the provided address.
func createBlocksToAddr(t *testing.T, numBlocks int, to common.Address) ([]*types.Block, []types.Receipts) {
	t.Helper()

	blocks := make([]*types.Block, numBlocks)
	receipts := make([]types.Receipts, numBlocks)
	parentHash := common.Hash{}

	for i := range numBlocks {
		header := &types.Header{
			ParentHash: parentHash,
			Number:     big.NewInt(int64(i)),
			Extra:      crypto.Keccak256(to.Bytes(), []byte{byte(i)}), // unique hash per block/recipient
		}
		tx := types.NewTx(&types.LegacyTx{Nonce: uint64(i), To: &to})
		block := types.NewBlock(header, []*types.Transaction{tx}, nil, nil, trie.NewStackTrie(nil))
		blocks[i] = block
		parentHash = block.Hash()

		receipt := &types.Receipt{TxHash: tx.Hash(), Logs: []*types.Log{{Address: to}}}
		receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
		receipts[i] = types.Receipts{receipt}
	}

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

func startPartialMigration(t *testing.T, db *Database, blocksToMigrate uint64) {
	t.Helper()

	n := uint64(0)
	db.migrator.headerDB = &blockingDatabase{
		HeightIndex: db.headerDB,
		shouldBlock: func() bool {
			n++
			return n > blocksToMigrate
		},
	}
	startMigration(t, db, false)
	require.Eventually(t, func() bool {
		return db.migrator.processed.Load() >= blocksToMigrate
	}, 5*time.Second, 100*time.Millisecond)
}

func startMigration(t *testing.T, db *Database, waitForCompletion bool) {
	t.Helper()

	db.migrator.completed.Store(false)
	require.NoError(t, db.StartMigration(t.Context()))

	if waitForCompletion {
		timeout := 5 * time.Second
		msg := fmt.Sprintf("Migration did not complete within timeout: %v", timeout)
		require.True(t, db.migrator.waitMigratorDone(timeout), msg)
		require.True(t, db.migrator.isCompleted())
	}
}
