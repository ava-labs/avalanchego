// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/trie"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// Config carries the optional knobs for [NewSyncer]. Log defaults to
// [logging.NoLog].
type Config struct {
	Log     logging.Logger
	Options []evmstate.SyncerOption
}

const (
	// leafsRequestType is fixed because it no longer affects the wire format. It
	// only selects which concrete message.LeafsRequest struct the graft syncer
	// builds; the leafClient adapter immediately flattens that to a single proto
	// GetLeafRequest, so Coreth vs SubnetEVM is indistinguishable on the wire. A
	// valid (non-zero) type is still required or message.NewLeafsRequest errors.
	leafsRequestType = message.CorethLeafsRequestType
	requestSize      = 1024
)

// NewSyncer builds an EVM state syncer that fetches trie leaves from peers on
// n, verifying each range proof before handing the leaves to the underlying
// graft state syncer.
func NewSyncer(n *p2p.Network, pt *p2p.PeerTracker, db ethdb.Database, root common.Hash, codeQueue *code.Queue, cfg Config) (types.Syncer, error) {
	log := cfg.Log
	if log == nil {
		log = logging.NoLog{}
	}
	g := &getter{
		client: NewClient(n, pt),
		log:    log,
	}
	return evmstate.NewSyncer(g, db, root, codeQueue, requestSize, leafsRequestType, cfg.Options...)
}

var _ types.LeafClient = (*getter)(nil)

type getter struct {
	client *Client
	log    logging.Logger
}

// GetLeafs implements [types.LeafClient]. It translates req to the proto
// wire request and retries until it receives a response whose range proof
// verifies against the requested root, or the context is cancelled.
func (g *getter) GetLeafs(ctx context.Context, req message.LeafsRequest) (message.LeafsResponse, error) {
	protoReq := &syncpb.GetLeafRequest{
		RootHash:    req.RootHash().Bytes(),
		AccountHash: req.AccountHash().Bytes(),
		StartKey:    req.StartKey(),
		EndKey:      req.EndKey(),
		KeyLimit:    uint32(req.KeyLimit()),
	}

	for {
		if ctx.Err() != nil {
			return message.LeafsResponse{}, ctx.Err()
		}

		resp := new(syncpb.GetLeafResponse)
		out, err := g.client.Send(ctx, protoReq, resp)
		if err != nil {
			g.log.Debug("sending leaf request", zap.Stringer("request", protoReq), zap.Error(err))
			continue // retry
		}

		leafs, err := g.checkResponse(protoReq, resp)
		if err != nil {
			g.log.Debug("checking leaf response", zap.Stringer("request", protoReq), zap.Error(err))
			out.Failure()
			continue // retry
		}
		out.Success()
		return leafs, nil
	}
}

var (
	errTooManyLeaves     = errors.New("response contains more than requested leaves")
	errEmptyResponse     = errors.New("empty response must include merkle proof")
	errInvalidRangeProof = errors.New("failed to verify range proof")
)

// checkResponse verifies that resp contains at most the requested number of
// leaves and that the key/value pairs form a valid Merkle range proof rooted
// at the requested root, over the requested [StartKey, lastKey] range.
func (g *getter) checkResponse(req *syncpb.GetLeafRequest, resp *syncpb.GetLeafResponse) (message.LeafsResponse, error) {
	limit := int(req.GetKeyLimit())
	if len(resp.Keys) > limit || len(resp.Values) > limit {
		return message.LeafsResponse{}, errTooManyLeaves
	}

	// An empty response (no more keys) must still carry a proof of absence.
	if len(resp.Keys) == 0 && len(resp.ProofVals) == 0 {
		return message.LeafsResponse{}, errEmptyResponse
	}

	// Populate the proof db from ProofVals. Leaving proof as a nil interface
	// when absent is valid: VerifyRangeProof then asserts every leaf under the
	// root is present.
	var proof ethdb.KeyValueReader
	if len(resp.ProofVals) > 0 {
		db := memorydb.New()
		defer db.Close()
		for _, proofVal := range resp.ProofVals {
			if err := db.Put(crypto.Keccak256(proofVal), proofVal); err != nil {
				return message.LeafsResponse{}, err
			}
		}
		proof = db
	}

	firstKey := req.GetStartKey()
	if len(resp.Keys) > 0 && len(firstKey) == 0 {
		lastKey := resp.Keys[len(resp.Keys)-1]
		firstKey = bytes.Repeat([]byte{0x00}, len(lastKey))
	}

	// VerifyRangeProof ensures the pairs are all of the keys in
	// [firstKey, lastKey] under the root, in monotonically increasing order,
	// and reports whether more keys exist to the right.
	root := common.BytesToHash(req.GetRootHash())
	more, err := trie.VerifyRangeProof(root, firstKey, resp.Keys, resp.Values, proof)
	if err != nil {
		return message.LeafsResponse{}, fmt.Errorf("%w: %w", errInvalidRangeProof, err)
	}

	return message.LeafsResponse{
		Keys: resp.Keys,
		Vals: resp.Values,
		More: more,
	}, nil
}
