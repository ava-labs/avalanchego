// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/saexec"
)

func (b *backend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	receipts, _, err := b.getReceipts(rpc.BlockNumberOrHashWithHash(hash, false))
	if err != nil {
		return nil, nil //nolint:nilerr // This follows geth behavior for [ethapi.Backend.GetReceipts]
	}
	return receipts, nil
}

// getReceipts resolves receipts and the underlying [types.Block] by number or
// hash, checking in-memory blocks first then falling back to the database.
// Returns nils for blocks that are not yet executed.
func (b *backend) getReceipts(numOrHash rpc.BlockNumberOrHash) (types.Receipts, *types.Block, error) {
	blk, err := readByNumberOrHash(
		b,
		numOrHash,
		func(b *blocks.Block) *blocks.Block {
			return b
		},
		func(db ethdb.Reader, h common.Hash, num uint64) (*blocks.Block, error) {
			if num > b.LastExecuted().Height() {
				return nil, blocks.ErrNotFound
			}
			blk, err := blocks.New(rawdb.ReadBlock(db, h, num), nil, nil, b.Logger())
			if err != nil {
				return nil, err
			}
			if err := blk.RestoreExecutionArtefacts(b.DB(), b.XDB(), b.ChainConfig()); err != nil {
				return nil, err
			}
			return blk, nil
		},
	)
	switch {
	case err != nil:
		// The use of [notFoundIsNil] in [readByNumberOrHash] means that we know
		// this is a "real" error, not just [blocks.ErrNotFound].
		return nil, nil, err
	case blk == nil || !blk.Executed():
		return nil, nil, nil
	default:
		return blk.Receipts(), blk.EthBlock(), nil
	}
}

type blockChainAPI struct {
	*ethapi.BlockChainAPI
	b *backend
}

// GetBlockReceipts overrides [ethapi.BlockChainAPI.GetBlockReceipts] to avoid
// returning an error when a user queries a known, but not yet executed, block.
func (b *blockChainAPI) GetBlockReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) ([]map[string]any, error) {
	receipts, blk, err := b.b.getReceipts(blockNrOrHash)
	if err != nil || blk == nil {
		return nil, nil //nolint:nilerr // This follows geth behavior for [ethapi.BlockChainAPI.GetBlockReceipts]
	}

	hash := blk.Hash()
	num := blk.NumberU64()
	signer := blocks.Signer(blk, b.b.ChainConfig())
	txs := blk.Transactions()

	result := make([]map[string]any, len(txs))
	for i, receipt := range receipts {
		result[i] = ethapi.MarshalReceipt(receipt, hash, num, signer, txs[i], i)
	}
	return result, nil
}

// PendingBlockAndReceipts returns a nil block and receipts. Returning nil tells
// geth that this backend does not support pending blocks. In SAE, the pending
// block is defined as the most recently accepted block, but receipts are only
// available after execution. Returning a non-nil block with incorrect or empty
// receipts could cause geth to encounter errors.
func (*backend) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return nil, nil
}

func (b *backend) GetLogs(ctx context.Context, blockHash common.Hash, number uint64) ([][]*types.Log, error) {
	return rawdb.ReadLogs(b.DB(), blockHash, number), nil
}

type immediateReceipts struct {
	recent func(context.Context, common.Hash) (*saexec.Receipt, bool, error)
	*ethapi.TransactionAPI
}

func (ir immediateReceipts) GetTransactionReceipt(ctx context.Context, h common.Hash) (map[string]any, error) {
	r, ok, err := ir.recent(ctx, h)
	if err != nil {
		return nil, err
	}
	if !ok {
		// The transaction has either not been included yet, or it was cleared
		// from the [saexec.Executor] cache but is on disk. The standard
		// mechanism already differentiates between these scenarios.
		return ir.TransactionAPI.GetTransactionReceipt(ctx, h)
	}
	return ethapi.MarshalReceipt(
		r.Receipt,
		r.BlockHash,
		r.BlockNumber.Uint64(),
		r.Signer,
		r.Tx,
		int(r.TransactionIndex), //nolint:gosec // Known to not overflow
	), nil
}
