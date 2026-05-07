// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/utils/set"
)

const epsilon = 1e-6

var (
	errNoPeers           = errors.New("no peers available")
	errMarshalRequest    = errors.New("marshal request")
	errSendRequest       = errors.New("send request")
	errHandlerFailed     = errors.New("handler request failed")
	errUnmarshalResponse = errors.New("unmarshal response")
)

// Client sends EVM state-sync requests over dedicated p2p handler IDs.
// Retry and response validation stay in the application layer.
type Client struct {
	leafs  *p2p.Client
	code   *p2p.Client
	blocks *p2p.Client
	peers  *p2p.PeerTracker
}

// NewClient builds a Client with a dedicated [p2p.Client] per request type,
// using peers for selection and bandwidth scoring.
func NewClient(p2pNet *p2p.Network, peers *p2p.PeerTracker) *Client {
	sampler := trackerSampler{peers: peers}
	return &Client{
		leafs:  p2pNet.NewClient(p2p.EVMLeafsRequestHandlerID, sampler),
		code:   p2pNet.NewClient(p2p.EVMCodeRequestHandlerID, sampler),
		blocks: p2pNet.NewClient(p2p.EVMBlockRequestHandlerID, sampler),
		peers:  peers,
	}
}

// GetLeafs sends a leaf-range request. Range-proof verification is the
// caller's responsibility.
func (c *Client) GetLeafs(ctx context.Context, req *syncpb.GetLeafsRequest) (*syncpb.LeafsResponse, error) {
	var resp syncpb.LeafsResponse
	if err := c.do(ctx, c.leafs, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetCode sends a code-by-hash request.
func (c *Client) GetCode(ctx context.Context, req *syncpb.GetCodeRequest) (*syncpb.CodeResponse, error) {
	var resp syncpb.CodeResponse
	if err := c.do(ctx, c.code, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetBlocks sends a block-batch request.
func (c *Client) GetBlocks(ctx context.Context, req *syncpb.GetBlockRequest) (*syncpb.BlockResponse, error) {
	var resp syncpb.BlockResponse
	if err := c.do(ctx, c.blocks, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// do is the shared synchronous dispatch for every request type.
func (c *Client) do(ctx context.Context, client *p2p.Client, req, resp proto.Message) error {
	requestBytes, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("%w: %w", errMarshalRequest, err)
	}

	nodeID, ok := c.peers.SelectPeer()
	if !ok {
		return errNoPeers
	}
	c.peers.RegisterRequest(nodeID)

	type result struct {
		bytes []byte
		err   error
	}
	resultCh := make(chan result, 1)
	onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
		resultCh <- result{bytes: responseBytes, err: err}
	}

	start := time.Now()
	if err := client.AppRequest(ctx, set.Of(nodeID), requestBytes, onResponse); err != nil {
		c.peers.RegisterFailure(nodeID)
		return fmt.Errorf("%w: %w", errSendRequest, err)
	}

	select {
	case <-ctx.Done():
		c.peers.RegisterFailure(nodeID)
		return ctx.Err()
	case r := <-resultCh:
		if r.err != nil {
			c.peers.RegisterFailure(nodeID)
			return fmt.Errorf("%w: %w", errHandlerFailed, r.err)
		}
		bandwidth := float64(len(r.bytes)) / (time.Since(start).Seconds() + epsilon)
		c.peers.RegisterResponse(nodeID, bandwidth)
		if err := proto.Unmarshal(r.bytes, resp); err != nil {
			return fmt.Errorf("%w: %w", errUnmarshalResponse, err)
		}
		return nil
	}
}

var _ p2p.NodeSampler = (*trackerSampler)(nil)

// trackerSampler adapts [p2p.PeerTracker] to [p2p.NodeSampler]. The Client
// itself sends to explicitly-selected peers, so this sampler is reached only
// if a future caller invokes AppRequestAny.
type trackerSampler struct {
	peers *p2p.PeerTracker
}

func (s trackerSampler) Sample(_ context.Context, limit int) []ids.NodeID {
	if limit <= 0 {
		return nil
	}
	nodeID, ok := s.peers.SelectPeer()
	if !ok {
		return nil
	}
	return []ids.NodeID{nodeID}
}
