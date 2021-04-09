package indexer

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

var (
	errInvalidIndexType = errors.New("invalid index type given")
	errInvalidChain     = errors.New("invalid chain given")
	errOnlyHaveBlocks   = errors.New("P-Chain and C-Chain only have blocks")
	errNoBlocks         = errors.New("X-Chain doesn't have blocks")
)

type IndexType byte

const (
	IndexTypeTransactions IndexType = iota
	IndexTypeVertices
	IndexTypeBlocks
)

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

type IndexedChain byte

const (
	XChain IndexedChain = iota
	PChain
	CChain
)

func (t IndexedChain) String() string {
	switch t {
	case XChain:
		return "X"
	case PChain:
		return "P"
	case CChain:
		return "C"
	}
	// Should never happen
	return "unknown"
}

type Client struct {
	rpc.EndpointRequester
}

// NewClient creates a client.
func NewClient(uri string, chain IndexedChain, indexType IndexType, requestTimeout time.Duration) (*Client, error) {
	switch {
	case chain == XChain && indexType == IndexTypeBlocks:
		return nil, errNoBlocks
	case (chain == PChain || chain == CChain) && indexType != IndexTypeBlocks:
		return nil, errOnlyHaveBlocks
	case chain != XChain && chain != PChain && chain != CChain:
		return nil, errInvalidChain
	case indexType != IndexTypeTransactions && indexType != IndexTypeVertices && indexType != IndexTypeBlocks:
		return nil, errInvalidIndexType
	}

	return &Client{
		EndpointRequester: rpc.NewEndpointRequester(uri, fmt.Sprintf("ext/index/%s/%s", chain, indexType), "index", requestTimeout),
	}, nil
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
