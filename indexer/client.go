// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for Avalanche Indexer API Endpoint
type Client interface {
	// GetContainerRange returns the transactions at index [startIndex], [startIndex+1], ... , [startIndex+n-1]
	// If [n] == 0, returns an empty response (i.e. null).
	// If [startIndex] > the last accepted index, returns an error (unless the above apply.)
	// If we run out of transactions, returns the ones fetched before running out.
	GetContainerRange(context.Context, *GetContainerRangeArgs, ...rpc.Option) ([]Container, error)
	// Get a container by its index
	GetContainerByIndex(context.Context, *GetContainer, ...rpc.Option) (Container, error)
	// Get the most recently accepted container
	GetLastAccepted(context.Context, *GetLastAcceptedArgs, ...rpc.Option) (Container, error)
	// Returns 1 less than the number of containers accepted on this chain
	GetIndex(context.Context, *GetIndexArgs, ...rpc.Option) (uint64, error)
	// Returns true if the given container is accepted
	IsAccepted(context.Context, *GetIndexArgs, ...rpc.Option) (bool, error)
	// Get a container by its index
	GetContainerByID(context.Context, *GetIndexArgs, ...rpc.Option) (Container, error)
}

// Client implementation for Avalanche Indexer API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient creates a client that can interact with an index via HTTP API calls.
// [host] is the host to make API calls to (e.g. http://1.2.3.4:9650).
// [endpoint] is the path to the index endpoint (e.g. /ext/index/C/block or /ext/index/X/tx).
func NewClient(host, endpoint string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(host, endpoint, "index"),
	}
}

func (c *client) GetContainerRange(ctx context.Context, args *GetContainerRangeArgs, options ...rpc.Option) ([]Container, error) {
	var fcs GetContainerRangeResponse
	if err := c.requester.SendRequest(ctx, "getContainerRange", args, &fcs, options...); err != nil {
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

func (c *client) GetContainerByIndex(ctx context.Context, args *GetContainer, options ...rpc.Option) (Container, error) {
	var fc FormattedContainer
	if err := c.requester.SendRequest(ctx, "getContainerByIndex", args, &fc, options...); err != nil {
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

func (c *client) GetLastAccepted(ctx context.Context, args *GetLastAcceptedArgs, options ...rpc.Option) (Container, error) {
	var fc FormattedContainer
	if err := c.requester.SendRequest(ctx, "getLastAccepted", args, &fc, options...); err != nil {
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

func (c *client) GetIndex(ctx context.Context, args *GetIndexArgs, options ...rpc.Option) (uint64, error) {
	var index GetIndexResponse
	err := c.requester.SendRequest(ctx, "getIndex", args, &index, options...)
	return uint64(index.Index), err
}

func (c *client) IsAccepted(ctx context.Context, args *GetIndexArgs, options ...rpc.Option) (bool, error) {
	var isAccepted bool
	err := c.requester.SendRequest(ctx, "isAccepted", args, &isAccepted, options...)
	return isAccepted, err
}

func (c *client) GetContainerByID(ctx context.Context, args *GetIndexArgs, options ...rpc.Option) (Container, error) {
	var fc FormattedContainer
	if err := c.requester.SendRequest(ctx, "getContainerByID", args, &fc, options...); err != nil {
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
