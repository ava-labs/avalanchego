// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

var _ Client = &client{}

// Client interface for Avalanche Indexer API Endpoint
type Client interface {
	// GetContainerRange returns the transactions at index [startIndex], [startIndex+1], ... , [startIndex+n-1]
	// If [n] == 0, returns an empty response (i.e. null).
	// If [startIndex] > the last accepted index, returns an error (unless the above apply.)
	// If we run out of transactions, returns the ones fetched before running out.
	GetContainerRange(ctx context.Context, startIndex uint64, numToFetch int, options ...rpc.Option) ([]Container, error)
	// Get a container by its index
	GetContainerByIndex(ctx context.Context, index uint64, options ...rpc.Option) (Container, error)
	// Get the most recently accepted container
	GetLastAccepted(context.Context, ...rpc.Option) (Container, error)
	// Returns 1 less than the number of containers accepted on this chain
	GetIndex(ctx context.Context, containerID ids.ID, options ...rpc.Option) (uint64, error)
	// Returns true if the given container is accepted
	IsAccepted(ctx context.Context, containerID ids.ID, options ...rpc.Option) (bool, error)
	// Get a container by its index
	GetContainerByID(ctx context.Context, containerID ids.ID, options ...rpc.Option) (Container, error)
}

// Client implementation for Avalanche Indexer API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient creates a client that can interact with an index via HTTP API
// calls.
// [uri] is the path to make API calls to.
// For example:
//   - http://1.2.3.4:9650/ext/index/C/block
//   - http://1.2.3.4:9650/ext/index/X/tx
func NewClient(uri string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "index"),
	}
}

func (c *client) GetContainerRange(ctx context.Context, startIndex uint64, numToFetch int, options ...rpc.Option) ([]Container, error) {
	var fcs GetContainerRangeResponse
	err := c.requester.SendRequest(ctx, "getContainerRange", &GetContainerRangeArgs{
		StartIndex: json.Uint64(startIndex),
		NumToFetch: json.Uint64(numToFetch),
		Encoding:   formatting.Hex,
	}, &fcs, options...)
	if err != nil {
		return nil, err
	}

	response := make([]Container, len(fcs.Containers))
	for i, resp := range fcs.Containers {
		containerBytes, err := formatting.Decode(resp.Encoding, resp.Bytes)
		if err != nil {
			return nil, fmt.Errorf("couldn't decode container %s: %w", resp.ID, err)
		}
		response[i] = Container{
			ID:        resp.ID,
			Timestamp: resp.Timestamp.Unix(),
			Bytes:     containerBytes,
		}
	}
	return response, nil
}

func (c *client) GetContainerByIndex(ctx context.Context, index uint64, options ...rpc.Option) (Container, error) {
	var fc FormattedContainer
	err := c.requester.SendRequest(ctx, "getContainerByIndex", &GetContainerByIndexArgs{
		Index:    json.Uint64(index),
		Encoding: formatting.Hex,
	}, &fc, options...)
	if err != nil {
		return Container{}, err
	}

	containerBytes, err := formatting.Decode(fc.Encoding, fc.Bytes)
	if err != nil {
		return Container{}, fmt.Errorf("couldn't decode container %s: %w", fc.ID, err)
	}
	return Container{
		ID:        fc.ID,
		Timestamp: fc.Timestamp.Unix(),
		Bytes:     containerBytes,
	}, nil
}

func (c *client) GetLastAccepted(ctx context.Context, options ...rpc.Option) (Container, error) {
	var fc FormattedContainer
	err := c.requester.SendRequest(ctx, "getLastAccepted", &GetLastAcceptedArgs{
		Encoding: formatting.Hex,
	}, &fc, options...)
	if err != nil {
		return Container{}, nil
	}

	containerBytes, err := formatting.Decode(fc.Encoding, fc.Bytes)
	if err != nil {
		return Container{}, fmt.Errorf("couldn't decode container %s: %w", fc.ID, err)
	}
	return Container{
		ID:        fc.ID,
		Timestamp: fc.Timestamp.Unix(),
		Bytes:     containerBytes,
	}, nil
}

func (c *client) GetIndex(ctx context.Context, id ids.ID, options ...rpc.Option) (uint64, error) {
	var index GetIndexResponse
	err := c.requester.SendRequest(ctx, "getIndex", &GetIndexArgs{
		ID: id,
	}, &index, options...)
	return uint64(index.Index), err
}

func (c *client) IsAccepted(ctx context.Context, id ids.ID, options ...rpc.Option) (bool, error) {
	var res IsAcceptedResponse
	err := c.requester.SendRequest(ctx, "isAccepted", &IsAcceptedArgs{
		ID: id,
	}, &res, options...)
	return res.IsAccepted, err
}

func (c *client) GetContainerByID(ctx context.Context, id ids.ID, options ...rpc.Option) (Container, error) {
	var fc FormattedContainer
	err := c.requester.SendRequest(ctx, "getContainerByID", &GetContainerByIDArgs{
		ID:       id,
		Encoding: formatting.Hex,
	}, &fc, options...)
	if err != nil {
		return Container{}, err
	}

	containerBytes, err := formatting.Decode(fc.Encoding, fc.Bytes)
	if err != nil {
		return Container{}, fmt.Errorf("couldn't decode container %s: %w", fc.ID, err)
	}
	return Container{
		ID:        fc.ID,
		Timestamp: fc.Timestamp.Unix(),
		Bytes:     containerBytes,
	}, nil
}
