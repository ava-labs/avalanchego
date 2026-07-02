// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	synctypes "github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/network/p2p"
	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	ethparams "github.com/ava-labs/libevm/params"
	"go.uber.org/zap"
)

type Config struct {
	Log logging.Logger
}

func (c Config) ApplyDefaults() Config {
	if c.Log == nil {
		c.Log = logging.NoLog{}
	}
	return c
}

// NewSyncer builds a code syncer that fetches code from peers on n and writes
// it to db as hashes arrive on codeHashes.
func NewSyncer(n *p2p.Network, pt *p2p.PeerTracker, db ethdb.Database, queue *code.Queue, cfg Config) (*code.Syncer, error) {
	cfg = cfg.ApplyDefaults()
	g := &getter{
		client: NewClient(n, pt),
		log:    cfg.Log,
	}
	return code.NewSyncer(g, db, queue.CodeHashes())
}

var _ synctypes.CodeClient = (*getter)(nil)

type getter struct {
	client *Client
	log    logging.Logger
}

// GetCode implements [synctypes.CodeClient]. It retries until it receives a
// response that passes checkResponse or the context is cancelled.
func (g *getter) GetCode(ctx context.Context, hashes []common.Hash) ([][]byte, error) {
	rawHashes := make([][]byte, len(hashes))
	for i, hash := range hashes {
		rawHashes[i] = hash.Bytes()
	}

	req := &syncpb.GetCodeRequest{Hashes: rawHashes}
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		resp := new(syncpb.GetCodeResponse)
		out, err := g.client.Send(ctx, req, resp)
		if err != nil {
			g.log.Debug("sending code request", zap.Stringer("request", req), zap.Error(err))
			continue // retry
		}

		code, err := g.checkResponse(resp, hashes)
		if err != nil {
			g.log.Debug("checking code response", zap.Stringer("request", req), zap.Error(err))
			out.Failure()
			continue // retry
		}
		out.Success()
		return code, nil
	}
}

var (
	errInvalidCodeResponseLen = errors.New("number of code bytes in response does not match requested hashes")
	errMaxCodeSizeExceeded    = errors.New("max code size exceeded")
	errHashMismatch           = errors.New("hash does not match expected value")
)

// checkResponse verifies that the response contains exactly one code blob per
// requested hash, each within the max code size, and each hashing to the hash
// it was requested by. The returned slices are ordered to match hashes.
func (g *getter) checkResponse(resp *syncpb.GetCodeResponse, hashes []common.Hash) ([][]byte, error) {
	if len(resp.Data) != len(hashes) {
		return nil, errInvalidCodeResponseLen
	}

	for i, code := range resp.Data {
		if len(code) > ethparams.MaxCodeSize {
			return nil, errMaxCodeSizeExceeded
		}

		if hash := crypto.Keccak256Hash(code); hash != hashes[i] {
			return nil, errHashMismatch
		}
	}
	return resp.Data, nil
}
