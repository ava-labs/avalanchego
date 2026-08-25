// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

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

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var (
	errEmptyLeafResponse = errors.New("empty leaf response must include a proof")
	errTooManyLeaves     = errors.New("more leaves returned than requested")
	errInvalidRangeProof = errors.New("invalid range proof")
)

// sender is the transport a [Client] sends over.
type sender = network.Dispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]

// newSender binds the leaf transport to handlerID on n.
func newSender(n *p2p.Network, handlerID uint64, peers *p2p.PeerTracker) *sender {
	return network.NewDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
		n,
		handlerID,
		peers,
	)
}

// Client reads verified leaf ranges over the proto protocol. A caller never
// sees a range that failed its proof.
type Client struct {
	log    logging.Logger
	sender *sender
}

// NewClient returns a [Client] reading handlerID's trie from n's peers.
func NewClient(log logging.Logger, n *p2p.Network, handlerID uint64, peers *p2p.PeerTracker) *Client {
	return &Client{log: log, sender: newSender(n, handlerID, peers)}
}

// LeafRange is a run of leaves to fetch from one trie.
type LeafRange struct {
	Root    common.Hash
	Account *common.Hash // nil for the account trie
	Start   []byte       // nil for the trie's first key
	Limit   uint16
}

// Leaves is a verified run of trie leaves in key order.
type Leaves struct {
	Keys [][]byte
	Vals [][]byte
}

// FetchLeaves re-requests from another peer until the range proves out or ctx
// ends, so an unproven range never surfaces.
func (c *Client) FetchLeaves(ctx context.Context, req LeafRange) (Leaves, bool, error) {
	reqPB := &syncpb.GetLeafRequest{
		RootHash: req.Root.Bytes(),
		StartKey: req.Start,
		KeyLimit: uint32(req.Limit),
	}
	if req.Account != nil {
		reqPB.AccountHash = req.Account.Bytes()
	}

	for {
		if err := ctx.Err(); err != nil {
			return Leaves{}, false, err
		}

		var resp syncpb.GetLeafResponse
		outcome, err := c.sender.Send(ctx, reqPB, &resp)
		if err != nil {
			// Send already de-scored the peer, re-request from another.
			c.log.Debug("leaf request failed, re-requesting",
				zap.Error(err),
			)
			continue
		}

		more, err := verifyRange(req, &resp)
		if err != nil {
			outcome.Failure()
			c.log.Debug("invalid leaf response, re-requesting",
				zap.Error(err),
			)
			continue
		}

		outcome.Success()
		return Leaves{
			Keys: resp.GetKeys(),
			Vals: resp.GetValues(),
		}, more, nil
	}
}

// verifyRange reports whether more leaves remain to the right of resp.
func verifyRange(req LeafRange, resp *syncpb.GetLeafResponse) (bool, error) {
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
