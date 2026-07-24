// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
)

func (b *backend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	receipts, _, err := b.getReceipts(ctx, rpc.BlockNumberOrHashWithHash(hash, false))
	if err != nil {
		return nil, nil //nolint:nilerr // This follows geth behavior for [ethapi.Backend.GetReceipts]
	}
	return receipts, nil
}

// getReceipts resolves receipts and the underlying [types.Block] by number or
// hash, checking in-memory blocks first then falling back to the database.
// Returns nils for blocks that are not yet executed.
func (b *backend) getReceipts(ctx context.Context, numOrHash rpc.BlockNumberOrHash) (types.Receipts, *types.Block, error) {
	blk, err := b.restoreExecutedBlock(ctx, numOrHash)
	switch {
	case errors.Is(err, blocks.ErrNotFound):
		return nil, nil, nil
	case err != nil:
		return nil, nil, err
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
	receipts, blk, err := b.b.getReceipts(ctx, blockNrOrHash)
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
		int(r.TransactionIndex), //#nosec G115 -- Known to not overflow
	), nil
}
