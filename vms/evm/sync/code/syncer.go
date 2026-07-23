// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

type Syncer = code.Syncer

func NewSyncer(network *p2p.Network, peerTracker *p2p.PeerTracker, db ethdb.Database, queue *code.Queue, log logging.Logger) (*Syncer, error) {
	client := newCodeClient(network, peerTracker, log)
	return code.NewSyncer(client, db, queue.CodeHashes())
}

var _ types.CodeClient = (*codeClient)(nil)

type codeClient struct {
	client *Client
	log    logging.Logger
}

func newCodeClient(n *p2p.Network, peers *p2p.PeerTracker, log logging.Logger) types.CodeClient {
	client := NewClient(n, peers)
	return &codeClient{client: client, log: log}
}

// GetCode implements [types.CodeClient].
func (c *codeClient) GetCode(ctx context.Context, hashes []common.Hash) ([][]byte, error) {
	req := &syncpb.GetCodeRequest{Hashes: hashBytes(hashes)}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp := &syncpb.GetCodeResponse{}
		outcome, err := c.client.Send(ctx, req, resp)
		if err != nil {
			// Send already de-scored the peer, re-request from another.
			continue
		}

		if err := verifyCode(hashes, resp.GetData()); err != nil {
			outcome.Failure()
			c.log.Debug("invalid code response, re-requesting", zap.Error(err))
			continue
		}

		outcome.Success()
		return resp.GetData(), nil
	}
}

var (
	errCodeCountMismatch = errors.New("code response count does not match requested hashes")
	errCodeSizeExceeded  = errors.New("max code size exceeded")
	errCodeHashMismatch  = errors.New("code does not hash to the requested value")
)

// verifyCode reports whether data is the code for hashes, in order.
func verifyCode(hashes []common.Hash, data [][]byte) error {
	if len(data) != len(hashes) {
		return fmt.Errorf("%w: got %d requested %d", errCodeCountMismatch, len(data), len(hashes))
	}
	for i, code := range data {
		if len(code) > params.MaxCodeSize {
			return fmt.Errorf("%w: hash %s size %d", errCodeSizeExceeded, hashes[i], len(code))
		}
		if got := crypto.Keccak256Hash(code); got != hashes[i] {
			return fmt.Errorf("%w at index %d: got %s requested %s", errCodeHashMismatch, i, got, hashes[i])
		}
	}
	return nil
}

func hashBytes(hashes []common.Hash) [][]byte {
	raw := make([][]byte, len(hashes))
	for i, h := range hashes {
		raw[i] = h.Bytes()
	}
	return raw
}
