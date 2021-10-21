// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

type Client struct {
	rpc.EndpointRequester
}

// NewClient creates a client that can interact with an index via HTTP API calls.
// [host] is the host to make API calls to (e.g. http://1.2.3.4:9650).
// [endpoint] is the path to the index endpoint (e.g. /ext/index/C/block or /ext/index/X/tx).
func NewClient(host, endpoint string, requestTimeout time.Duration) *Client {
	return &Client{
		EndpointRequester: rpc.NewEndpointRequester(host, endpoint, "index", requestTimeout),
	}
}

func (c *Client) GetContainerRange(args *GetContainerRangeArgs) ([]Container, error) {
	var fcs GetContainerRangeResponse
	if err := c.SendRequest("getContainerRange", args, &fcs); err != nil {
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

func (c *Client) GetContainerByIndex(args *GetContainer) (Container, error) {
	var fc FormattedContainer
	if err := c.SendRequest("getContainerByIndex", args, &fc); err != nil {
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

func (c *Client) GetLastAccepted(args *GetLastAcceptedArgs) (Container, error) {
	var fc FormattedContainer
	if err := c.SendRequest("getLastAccepted", args, &fc); err != nil {
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

func (c *Client) GetIndex(args *GetIndexArgs) (GetIndexResponse, error) {
	var index GetIndexResponse
	err := c.SendRequest("getIndex", args, &index)
	return index, err
}

func (c *Client) IsAccepted(args *GetIndexArgs) (bool, error) {
	var isAccepted bool
	err := c.SendRequest("isAccepted", args, &isAccepted)
	return isAccepted, err
}

func (c *Client) GetContainerByID(args *GetIndexArgs) (Container, error) {
	var fc FormattedContainer
	if err := c.SendRequest("getContainerByID", args, &fc); err != nil {
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
