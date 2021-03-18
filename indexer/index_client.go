package indexer

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

type IndexType byte

const (
	IndexTypeTransactions IndexType = iota
	IndexTypeVertices
	IndexTypeBlocks
)

type Client struct {
	rpc.EndpointRequester
}

// NewClient creates a client.
func NewClient(uri string, indexType fmt.Stringer, requestTimeout time.Duration) *Client {
	return &Client{
		EndpointRequester: rpc.NewEndpointRequester(uri, fmt.Sprintf("ext/index/%s", indexType), "index", requestTimeout),
	}
}

func (t IndexType) String() string {
	switch t {
	case IndexTypeTransactions:
		return "tx"
	case IndexTypeVertices:
		return "vtx"
	case IndexTypeBlocks:
		return "block"
	}
	return "unknown"
}

func (c *Client) GetContainerRange(args *GetContainerRange) ([]FormattedContainer, error) {
	var response []FormattedContainer
	err := c.SendRequest("getContainerRange", args, &response)
	return response, err
}

func (c *Client) GetContainerByIndex(args *GetContainer) (FormattedContainer, error) {
	var response FormattedContainer
	err := c.SendRequest("getContainerByIndex", args, &response)
	return response, err
}

func (c *Client) GetLastAccepted(args *GetLastAcceptedArgs) (FormattedContainer, error) {
	var response FormattedContainer
	err := c.SendRequest("getLastAccepted", args, &response)
	return response, err
}

func (c *Client) GetIndex(args *GetIndexArgs) (GetIndexResponse, error) {
	var response GetIndexResponse
	err := c.SendRequest("getIndex", args, &response)
	return response, err
}

func (c *Client) IsAccepted(args *GetIndexArgs) (bool, error) {
	var response bool
	err := c.SendRequest("isAccepted", args, &response)
	return response, err
}
