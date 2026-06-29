// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

func (b *backend) CurrentBlock() *types.Header {
	return b.CurrentHeader()
}

func (b *backend) HeaderByNumber(ctx context.Context, n rpc.BlockNumber) (*types.Header, error) {
	return readByNumber(b, n, rawdb.ReadHeader)
}

func (b *backend) BlockByNumber(ctx context.Context, n rpc.BlockNumber) (*types.Block, error) {
	return readByNumber(b, n, rawdb.ReadBlock)
}

func (b *backend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return readByHash(b, hash, (*blocks.Block).Header, rawdb.ReadHeader)
}

func (b *backend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return readByHash(b, hash, (*blocks.Block).EthBlock, rawdb.ReadBlock)
}

func (b *backend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	return readByNumberOrHash(b, blockNrOrHash, (*blocks.Block).Header, neverErrs(rawdb.ReadHeader))
}

func (b *backend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	return readByNumberOrHash(b, blockNrOrHash, (*blocks.Block).EthBlock, neverErrs(rawdb.ReadBlock))
}

func (b *backend) GetBody(ctx context.Context, hash common.Hash, number rpc.BlockNumber) (*types.Body, error) {
	return readByNumberAndHash(b, hash, number, (*blocks.Block).Body, rawdb.ReadBody)
}

// restoreBlock finds any available block by number or hash. All methods unrelated to ancestry
// are safe to use.
func (b *backend) restoreBlock(numOrHash rpc.BlockNumberOrHash) (*blocks.Block, error) {
	return readByNumberOrHash(
		b,
		numOrHash,
		func(b *blocks.Block) *blocks.Block {
			return b
		},
		func(db ethdb.Reader, h common.Hash, num uint64) (*blocks.Block, error) {
			if num >= b.LastSettled().Height() {
				// We can't restore the settlement state, it should have been found in the map.
				return nil, blocks.ErrNotFound
			}
			ethB := rawdb.ReadBlock(db, h, num)
			if ethB == nil {
				return nil, blocks.ErrNotFound
			}
			return blocks.RestoreSettledBlock(ethB, b.Hooks(), b.Logger(), b.DB(), b.XDB(), b.ChainConfig())
		},
	)
}
