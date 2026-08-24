// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leafproto

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var (
	_ types.LeafFetcher = (*Client)(nil)

	errEmptyLeafResponse = errors.New("empty leaf response must include a proof")
	errTooManyLeaves     = errors.New("more leaves returned than requested")
	errInvalidRangeProof = errors.New("invalid range proof")
)

// sender is the transport a [Client] sends over.
type sender interface {
	Send(ctx context.Context, req *syncpb.GetLeafRequest, resp *syncpb.GetLeafResponse) (*network.Outcome, error)
}

// Client reads state-trie leaves over the proto protocol.
type Client struct {
	log    logging.Logger
	sender sender
}

func NewClient(log logging.Logger, s sender) *Client {
	return &Client{log: log, sender: s}
}

// FetchLeaves re-requests from another peer until the range proves out or ctx
// ends, so an unproven range never surfaces.
func (c *Client) FetchLeaves(ctx context.Context, req types.LeafRange) (types.Leaves, bool, error) {
	pbReq := &syncpb.GetLeafRequest{
		RootHash:    req.Root.Bytes(),
		AccountHash: accountBytes(req.Account),
		StartKey:    req.Start,
		EndKey:      req.End,
		KeyLimit:    uint32(req.Limit),
	}

	for {
		if err := ctx.Err(); err != nil {
			return types.Leaves{}, false, err
		}

		pbResp := &syncpb.GetLeafResponse{}
		outcome, err := c.sender.Send(ctx, pbReq, pbResp)
		if err != nil {
			// Send already de-scored the peer, re-request from another.
			continue
		}

		more, err := verifyRange(req, pbResp)
		if err != nil {
			outcome.Failure()
			c.log.Debug("invalid leaf response, re-requesting", zap.Error(err))
			continue
		}

		outcome.Success()
		return types.Leaves{Keys: pbResp.GetKeys(), Vals: pbResp.GetValues()}, more, nil
	}
}

// verifyRange reports whether more leaves remain to the right of resp.
func verifyRange(req types.LeafRange, resp *syncpb.GetLeafResponse) (bool, error) {
	keys, vals, proofVals := resp.GetKeys(), resp.GetValues(), resp.GetProofVals()
	if len(keys) > int(req.Limit) {
		return false, fmt.Errorf("%w: got %d want at most %d", errTooManyLeaves, len(keys), req.Limit)
	}
	if len(keys) == 0 && len(proofVals) == 0 {
		return false, errEmptyLeafResponse
	}

	// A whole-trie response carries no proof, so VerifyRangeProof asserts the
	// keys are the complete trie for the root. Otherwise rebuild it by hash.
	var proof ethdb.Database
	if len(proofVals) > 0 {
		proof = rawdb.NewMemoryDatabase()
		defer proof.Close()
		for _, val := range proofVals {
			if err := proof.Put(crypto.Keccak256(val), val); err != nil {
				return false, err
			}
		}
	}

	// A nil start means the trie's beginning, which VerifyRangeProof wants zero-padded.
	firstKey := req.Start
	if firstKey == nil && len(keys) > 0 {
		firstKey = make([]byte, len(keys[0]))
	}

	more, err := trie.VerifyRangeProof(req.Root, firstKey, keys, vals, proof)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errInvalidRangeProof, err)
	}
	return more, nil
}

func accountBytes(account common.Hash) []byte {
	if account == (common.Hash{}) {
		return nil
	}
	return account.Bytes()
}
