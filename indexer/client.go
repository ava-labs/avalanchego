// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"fmt"
	"time"

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
	GetContainerRange(*GetContainerRangeArgs) ([]Container, error)
	// Get a container by its index
	GetContainerByIndex(*GetContainer) (Container, error)
	// Get the most recently accepted container
	GetLastAccepted(*GetLastAcceptedArgs) (Container, error)
	// Returns 1 less than the number of containers accepted on this chain
	GetIndex(*GetIndexArgs) (uint64, error)
	// Returns true if the given container is accepted
	IsAccepted(*GetIndexArgs) (bool, error)
	// Get a container by its index
	GetContainerByID(*GetIndexArgs) (Container, error)
}

// Client implementation for Avalanche Indexer API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient creates a client that can interact with an index via HTTP API calls.
// [host] is the host to make API calls to (e.g. http://1.2.3.4:9650).
// [endpoint] is the path to the index endpoint (e.g. /ext/index/C/block or /ext/index/X/tx).
func NewClient(host, endpoint string, requestTimeout time.Duration) Client {
	return &client{
		requester: rpc.NewEndpointRequester(host, endpoint, "index", requestTimeout),
	}
}

func (c *client) GetContainerRange(args *GetContainerRangeArgs) ([]Container, error) {
	var fcs GetContainerRangeResponse
	if err := c.requester.SendRequest("getContainerRange", args, &fcs); err != nil {
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

func (c *client) GetContainerByIndex(args *GetContainer) (Container, error) {
	var fc FormattedContainer
	if err := c.requester.SendRequest("getContainerByIndex", args, &fc); err != nil {
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

func (c *client) GetLastAccepted(args *GetLastAcceptedArgs) (Container, error) {
	var fc FormattedContainer
	if err := c.requester.SendRequest("getLastAccepted", args, &fc); err != nil {
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

func (c *client) GetIndex(args *GetIndexArgs) (uint64, error) {
	var index GetIndexResponse
	err := c.requester.SendRequest("getIndex", args, &index)
	return uint64(index.Index), err
}

func (c *client) IsAccepted(args *GetIndexArgs) (bool, error) {
	var isAccepted bool
	err := c.requester.SendRequest("isAccepted", args, &isAccepted)
	return isAccepted, err
}

func (c *client) GetContainerByID(args *GetIndexArgs) (Container, error) {
	var fc FormattedContainer
	if err := c.requester.SendRequest("getContainerByID", args, &fc); err != nil {
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
