// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"bytes"
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

// sender is the transport a [Client] sends requests over.
type sender = network.Dispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]

// Client reads verified leaf ranges over the proto protocol. A caller never
// sees a range that failed its proof.
type Client struct {
	log    logging.Logger
	sender *sender
	minKey []byte // read-only stand-in for an absent start key
}

// NewClient returns a [Client] reading handlerID's trie from n's peers.
func NewClient(
	log logging.Logger,
	n *p2p.Network,
	handlerID uint64,
	trieKeyLength int,
	peers *p2p.PeerTracker,
) *Client {
	return &Client{
		log: log,
		sender: network.NewDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
			n,
			handlerID,
			peers,
		),
		minKey: make([]byte, trieKeyLength),
	}
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

// FetchLeaves returns a proven run of leaves and whether the trie holds leaves
// past it. It re-requests from another peer until the range proves out or ctx
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

		more, err := verifyRange(c.minKey, req, &resp)
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

var (
	errTooManyLeaves     = errors.New("more leaves returned than requested")
	errKeyBeforeStart    = errors.New("key returned before the requested start")
	errInvalidRangeProof = errors.New("invalid range proof")
)

// verifyRange proves that resp holds exactly the trie's leaves from start, and
// reports whether the trie holds leaves past resp.
func verifyRange(zero []byte, req LeafRange, resp *syncpb.GetLeafResponse) (bool, error) {
	keys := resp.GetKeys()
	if len(keys) > int(req.Limit) {
		return false, fmt.Errorf("%w: got %d want at most %d", errTooManyLeaves, len(keys), req.Limit)
	}

	// The request keeps the empty start key to save wire bytes.
	// [trie.VerifyRangeProof] needs the zero-padded form.
	start := req.Start
	if len(start) == 0 {
		start = zero
	}
	if len(keys) > 0 && bytes.Compare(keys[0], start) < 0 {
		return false, errKeyBeforeStart
	}

	// A whole-trie response carries no proof, so VerifyRangeProof asserts the
	// keys are the complete trie for the root. Otherwise rebuild it by hash.
	var proof ethdb.Database
	if nodes := resp.GetProofVals(); len(nodes) > 0 {
		proof = rawdb.NewMemoryDatabase()
		for _, node := range nodes {
			if err := proof.Put(crypto.Keccak256(node), node); err != nil {
				return false, err
			}
		}
	}

	more, err := trie.VerifyRangeProof(
		req.Root,
		start,
		keys,
		resp.GetValues(),
		proof,
	)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errInvalidRangeProof, err)
	}
	return more, nil
}
