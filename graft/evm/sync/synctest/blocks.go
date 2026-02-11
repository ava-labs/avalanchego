// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"
)

var (
	// testKey is a pre-generated private key for test transactions.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the address derived from testKey.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

// BlockGeneratorConfig configures block generation.
type BlockGeneratorConfig struct {
	// TxDataSize sets a fixed size for transaction data in each block.
	// If TxDataSizeFunc is set, this field is ignored.
	TxDataSize int

	// TxDataSizeFunc returns the transaction data size for a given block index.
	// Block index 0 is the genesis block (which has no transactions).
	// If nil, TxDataSize is used for all blocks.
	TxDataSizeFunc func(blockIndex int) int

	// GasLimit sets the gas limit for generated blocks.
	// Defaults to params.GenesisGasLimit if zero.
	GasLimit uint64
}

// GenerateTestBlocks creates a chain of test blocks for sync testing.
// Returns a slice of blocks where blocks[0] is the genesis block.
//
// Each non-genesis block contains a single signed transaction to ensure
// unique block hashes. The blocks are minimal but structurally valid
// for testing block sync functionality.
func GenerateTestBlocks(t *testing.T, numBlocks int, cfg *BlockGeneratorConfig) []*types.Block {
	t.Helper()

	if cfg == nil {
		cfg = &BlockGeneratorConfig{}
	}

	gasLimit := cfg.GasLimit
	if gasLimit == 0 {
		gasLimit = params.GenesisGasLimit
	}

	blocks := make([]*types.Block, numBlocks+1)
	blocks[0] = newGenesisBlock(gasLimit)

	for i := 1; i <= numBlocks; i++ {
		txDataSize := cfg.TxDataSize
		if cfg.TxDataSizeFunc != nil {
			txDataSize = cfg.TxDataSizeFunc(i)
		}
		blocks[i] = newBlock(t, blocks[i-1], i, txDataSize, gasLimit)
	}

	return blocks
}

func newGenesisBlock(gasLimit uint64) *types.Block {
	header := &types.Header{
		Number:      big.NewInt(0),
		Difficulty:  big.NewInt(1),
		GasLimit:    gasLimit,
		Time:        0,
		Extra:       []byte{},
		Root:        types.EmptyRootHash,
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		UncleHash:   types.EmptyUncleHash,
	}
	return types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
}

func newBlock(t *testing.T, parent *types.Block, blockNum, txDataSize int, gasLimit uint64) *types.Block {
	t.Helper()

	txData := makeTxData(blockNum, txDataSize)
	signedTx := newSignedTx(t, blockNum, txData)

	header := &types.Header{
		ParentHash:  parent.Hash(),
		Number:      big.NewInt(int64(blockNum)),
		Difficulty:  big.NewInt(1),
		GasLimit:    gasLimit,
		GasUsed:     signedTx.Gas(),
		Time:        uint64(blockNum * 10), // 10 seconds between blocks
		Extra:       []byte{},
		Root:        types.EmptyRootHash,
		ReceiptHash: types.EmptyReceiptsHash,
		UncleHash:   types.EmptyUncleHash,
	}

	return types.NewBlock(header, []*types.Transaction{signedTx}, nil, nil, trie.NewStackTrie(nil))
}

func makeTxData(blockNum, size int) []byte {
	if size <= 0 {
		return nil
	}
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(blockNum + i)
	}
	return data
}

func newSignedTx(t *testing.T, blockNum int, txData []byte) *types.Transaction {
	t.Helper()

	// Use non-zero gas price to ensure gas cost is calculated.
	// Calldata gas: 16 per non-zero byte, 4 per zero byte.
	// We use 16 as upper bound since makeTxData produces non-zero bytes.
	calldataGas := uint64(len(txData)) * params.TxDataNonZeroGasEIP2028

	tx := types.NewTransaction(
		uint64(blockNum-1), // nonce
		testAddr,
		big.NewInt(10),
		params.TxGas+calldataGas,
		big.NewInt(1),
		txData,
	)

	signedTx, err := types.SignTx(tx, types.HomesteadSigner{}, testKey)
	require.NoError(t, err)
	return signedTx
}
