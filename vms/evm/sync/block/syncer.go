// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/graft/evm/sync/block"
	synctypes "github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/network/p2p"
	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"go.uber.org/zap"
)

type Config struct {
	BlocksToFetch          uint64
	Log                    logging.Logger
	ExtraBlockVerification BlockVerifier
}

type BlockVerifier func(*types.Block) error

const minBlocksToFetch = 256

func (c Config) ApplyDefaults() Config {
	if c.BlocksToFetch == 0 {
		c.BlocksToFetch = minBlocksToFetch
	}
	if c.Log == nil {
		c.Log = logging.NoLog{}
	}
	return c
}

func NewSyncer(n *p2p.Network, pt *p2p.PeerTracker, db ethdb.Database, fromHash common.Hash, fromHeight uint64, cfg Config) (*block.BlockSyncer, error) {
	cfg = cfg.ApplyDefaults()
	g := &getter{
		client:     NewClient(n, pt),
		log:        cfg.Log,
		extraCheck: cfg.ExtraBlockVerification,
	}
	return block.NewSyncer(g, db, fromHash, fromHeight, cfg.BlocksToFetch)
}

var _ synctypes.BlockClient = (*getter)(nil)

type getter struct {
	client     *Client
	log        logging.Logger
	extraCheck BlockVerifier
}

// GetBlocks implements [synctypes.BlockClient] for the legacy block syncer.
// Any error returned is treated as fatal.
func (g *getter) GetBlocks(ctx context.Context, blockHash common.Hash, height uint64, parents uint16) ([]*types.Block, error) {
	req := &syncpb.GetBlockRequest{
		Height:     height,
		NumParents: uint32(parents),
	}
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		resp := new(syncpb.GetBlockResponse)
		out, err := g.client.Send(ctx, req, resp)
		if err != nil {
			g.log.Debug("sending block request", zap.Stringer("request", req), zap.Error(err))
			continue // retry
		}

		blocks, err := g.checkResponse(resp, blockHash)
		if err != nil {
			g.log.Debug("checking block response", zap.Stringer("request", req), zap.Error(err))
			out.Failure()
			continue // retry
		}
		out.Success()
		return blocks, nil
	}
}

var (
	errEmptyResponse = errors.New("empty response")
	errHashMismatch  = errors.New("hash does not match expected value")
)

func (g *getter) checkResponse(resp *syncpb.GetBlockResponse, startHash common.Hash) ([]*types.Block, error) {
	if len(resp.Blocks) == 0 {
		return nil, errEmptyResponse
	}

	blocks := make([]*types.Block, len(resp.Blocks))
	expectedHash := startHash
	for i, b := range resp.Blocks {
		ethBlock := new(types.Block)
		if err := rlp.DecodeBytes(b, ethBlock); err != nil {
			return nil, err
		}

		if hash := ethBlock.Hash(); hash != expectedHash {
			return nil, errHashMismatch
		}

		if g.extraCheck != nil {
			if err := g.extraCheck(ethBlock); err != nil {
				return nil, err
			}
		}

		blocks[i] = ethBlock
		expectedHash = ethBlock.ParentHash()
	}
	return blocks, nil
}
