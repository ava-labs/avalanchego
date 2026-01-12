// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"context"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"

	sharedmemorypb "github.com/ava-labs/avalanchego/proto/pb/sharedmemory"
)

var _ atomic.SharedMemory = (*Client)(nil)

// Client is atomic.SharedMemory that talks over RPC.
type Client struct {
	client sharedmemorypb.SharedMemoryClient
}

// NewClient returns shared memory connected to remote shared memory
func NewClient(client sharedmemorypb.SharedMemoryClient) *Client {
	return &Client{client: client}
}

func (c *Client) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	resp, err := c.client.Get(context.Background(), &sharedmemorypb.GetRequest{
		PeerChainId: peerChainID[:],
		Keys:        keys,
	})
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (c *Client) Indexed(
	peerChainID ids.ID,
	traits [][]byte,
	startTrait,
	startKey []byte,
	limit int,
) (
	[][]byte,
	[]byte,
	[]byte,
	error,
) {
	resp, err := c.client.Indexed(context.Background(), &sharedmemorypb.IndexedRequest{
		PeerChainId: peerChainID[:],
		Traits:      traits,
		StartTrait:  startTrait,
		StartKey:    startKey,
		Limit:       int32(limit),
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return resp.Values, resp.LastTrait, resp.LastKey, nil
}

func (c *Client) Apply(requests map[ids.ID]*atomic.Requests, batches ...database.Batch) error {
	req := &sharedmemorypb.ApplyRequest{
		Requests: make([]*sharedmemorypb.AtomicRequest, 0, len(requests)),
		Batches:  make([]*sharedmemorypb.Batch, len(batches)),
	}
	for key, value := range requests {
		chainReq := &sharedmemorypb.AtomicRequest{
			RemoveRequests: value.RemoveRequests,
			PutRequests:    make([]*sharedmemorypb.Element, len(value.PutRequests)),
			PeerChainId:    key[:],
		}
		for i, v := range value.PutRequests {
			chainReq.PutRequests[i] = &sharedmemorypb.Element{
				Key:    v.Key,
				Value:  v.Value,
				Traits: v.Traits,
			}
		}
		req.Requests = append(req.Requests, chainReq)
	}
	for i, batch := range batches {
		batch := batch.Inner()
		fb := filteredBatch{
			writes: make(map[string][]byte),
		}
		if err := batch.Replay(&fb); err != nil {
			return err
		}
		req.Batches[i] = &sharedmemorypb.Batch{
			Puts:    fb.PutRequests(),
			Deletes: fb.DeleteRequests(),
		}
	}

	_, err := c.client.Apply(context.Background(), req)
	return err
}
