// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
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
	type result struct {
		receipts types.Receipts
		block    *types.Block
	}
	r, err := readByNumberOrHash[result](
		b,
		numOrHash,
		func(b *blocks.Block) *result {
			if !b.Executed() {
				return &result{}
			}
			return &result{
				receipts: b.Receipts(),
				block:    b.EthBlock(),
			}
		},
		func(db ethdb.Reader, h common.Hash, num uint64) (*result, error) {
			if num > b.LastExecuted().Height() {
				return nil, blocks.ErrNotFound
			}
			rawBlk := rawdb.ReadBlock(db, h, num)
			// TODO(StephenButtolph): Can panic after state-sync
			if b.isSynchronous(rawBlk.NumberU64()) {
				receipts := rawdb.ReadRawReceipts(db, rawBlk.Hash(), rawBlk.NumberU64())
				if err := receipts.DeriveFields(
					b.ChainConfig(),
					rawBlk.Hash(),
					rawBlk.NumberU64(),
					rawBlk.Time(),
					rawBlk.BaseFee(),
					nil, // SAE does not support blob transactions.
					rawBlk.Transactions(),
				); err != nil {
					return nil, fmt.Errorf("deriving receipt fields: %v", err)
				}
				return &result{
					receipts: receipts,
					block:    rawBlk,
				}, nil
			}

			blk, err := blocks.New(rawBlk, nil, nil, b.Logger())
			if err != nil {
				return nil, err
			}
			if err := blk.RestoreExecutionArtefacts(b.DB(), b.XDB(), b.ChainConfig()); err != nil {
				return nil, err
			}
			return &result{
				receipts: blk.Receipts(),
				block:    blk.EthBlock(),
			}, nil
		},
	)
	if err != nil {
		return nil, nil, err
	}
	if r == nil {
		return nil, nil, nil
	}
	return r.receipts, r.block, nil
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
		int(r.TransactionIndex), //#nosec G115 -- Known to not overflow
	), nil
}
